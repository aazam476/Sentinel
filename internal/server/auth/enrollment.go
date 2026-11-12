// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// BeginTOTPEnrollment creates a new TOTP secret for a user who signed in
// with their password but has no second factor yet, where the server
// requires one (SEC-04: such a user can do nothing else until they set one
// up), or for a signed-in user adding an authenticator app from their
// account after a recent check (SEC-07). Calling it again replaces the
// unconfirmed secret.
func (s *Service) BeginTOTPEnrollment(ctx context.Context, session Session) (TOTPEnrollment, error) {
	if err := s.canEnrollTOTP(session); err != nil {
		return TOTPEnrollment{}, err
	}
	enrollment, err := newTOTPEnrollment(session.Username)
	if err != nil {
		return TOTPEnrollment{}, err
	}

	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		return s.saveTOTPSecret(ctx, tx, session.UserID, enrollment.Secret, nil)
	})
	if err != nil {
		return TOTPEnrollment{}, err
	}
	return enrollment, nil
}

// ConfirmTOTPEnrollment checks a code from the newly added authenticator,
// confirms the secret, issues ten recovery codes and signs the user in.
func (s *Service) ConfirmTOTPEnrollment(ctx context.Context, session Session, code string, request RequestInfo) (SignedIn, error) {
	if err := s.canEnrollTOTP(session); err != nil {
		return SignedIn{}, err
	}
	if err := s.checkRateLimit(ctx, session.Username, request); err != nil {
		return SignedIn{}, err
	}

	var signedIn SignedIn
	var codeValid bool
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		step, valid, err := s.checkEnrollmentCode(ctx, tx, session.UserID, code)
		if err != nil || !valid {
			return err
		}
		codeValid = true
		signedIn, err = s.finishEnrollment(ctx, tx, session, step, request)
		return err
	})
	if err != nil {
		return SignedIn{}, err
	}
	if !codeValid {
		return SignedIn{}, s.recordFailedAttempt(ctx, failedAttempt{user: User{ID: session.UserID, Username: session.Username},
			username: session.Username, request: request, action: "auth.totp.enroll", reason: "invalid_code", err: ErrInvalidCode})
	}
	s.resetRateLimit(session.Username)
	return signedIn, nil
}

// canEnrollTOTP allows adding an authenticator app while signing in
// without any second factor, or when signed in without one recently
// checked.
func (s *Service) canEnrollTOTP(session Session) error {
	if !session.IsSignedIn() {
		if session.HasSecondFactor {
			return ErrWrongLoginStep
		}
		return nil
	}
	if session.HasTOTP {
		return ErrWrongLoginStep
	}
	return s.RequireRecentSecondFactor(session)
}

// checkEnrollmentCode verifies code against the user's unconfirmed secret.
func (s *Service) checkEnrollmentCode(ctx context.Context, tx *sql.Tx, userID int64, code string) (step int64, valid bool, err error) {
	stored, err := s.loadTOTPSecret(ctx, tx, userID)
	if errors.Is(err, errNoTOTPSecret) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	step, valid = verifyTOTP(stored.secret, strings.TrimSpace(code), s.clock.Now(), stored.lastUsedStep)
	return step, valid, nil
}

func (s *Service) finishEnrollment(ctx context.Context, tx *sql.Tx, session Session, step int64, request RequestInfo) (SignedIn, error) {
	if err := confirmTOTPSecret(ctx, tx, session.UserID, s.now(), step); err != nil {
		return SignedIn{}, err
	}
	codes, hashes := newRecoveryCodes()
	if err := replaceRecoveryCodes(ctx, tx, session.UserID, hashes); err != nil {
		return SignedIn{}, err
	}
	token, err := s.replaceSession(ctx, tx, session, request)
	if err != nil {
		return SignedIn{}, err
	}
	entry := forUser(auditEntry(request, "auth.totp.enroll", audit.OutcomeSuccess, nil), session.UserID, session.Username)
	if err := s.audit.RecordInTx(ctx, tx, entry); err != nil {
		return SignedIn{}, err
	}
	return SignedIn{SessionToken: token, RecoveryCodes: codes}, nil
}

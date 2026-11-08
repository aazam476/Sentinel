// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// NextStep says what a session must do after the password step.
type NextStep string

// The steps after a correct password (SEC-04: 2FA is never optional).
const (
	// NextStepSecondFactor: enter a TOTP or recovery code.
	NextStepSecondFactor NextStep = "second_factor"
	// NextStepEnrollSecondFactor: the user has no 2FA yet and must set it up
	// before they can do anything else.
	NextStepEnrollSecondFactor NextStep = "enroll_second_factor"
)

// LoginInput is the password step of signing in.
type LoginInput struct {
	Username string
	Password string
}

// LoginResult is a session that has passed the password step.
type LoginResult struct {
	SessionToken string
	NextStep     NextStep
}

// SecondFactorInput carries one of the second-factor codes.
type SecondFactorInput struct {
	TOTPCode     string
	RecoveryCode string
}

// Login checks a username and password (SEC-04, SEC-05). On success it
// starts a session that can only complete the login: it has no access to
// anything else until the second factor is done.
func (s *Service) Login(ctx context.Context, input LoginInput, request RequestInfo) (LoginResult, error) {
	username := strings.TrimSpace(input.Username)
	if err := s.checkRateLimit(ctx, username, request); err != nil {
		return LoginResult{}, err
	}

	user, err := s.findUserForLogin(ctx, username)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.checkNotLocked(user); err != nil {
		return LoginResult{}, s.auditRefusal(ctx, user, username, request, err)
	}

	matches, err := s.hasher.Verify(ctx, input.Password, user.passwordHash)
	if err != nil {
		return LoginResult{}, err
	}
	if !matches || user.DisabledAt != nil {
		return LoginResult{}, s.recordFailedAttempt(ctx, failedAttempt{user: user, username: username, request: request,
			action: "auth.login.password", reason: "invalid_credentials", err: ErrInvalidCredentials})
	}

	return s.startPasswordSession(ctx, user, request)
}

func (s *Service) startPasswordSession(ctx context.Context, user User, request RequestInfo) (LoginResult, error) {
	var result LoginResult
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		token, err := s.startSession(ctx, tx, user.ID, nil, request)
		if err != nil {
			return err
		}
		result.SessionToken = token
		secondFactorReady, err := hasSecondFactor(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		result.NextStep = NextStepSecondFactor
		if !secondFactorReady {
			result.NextStep = NextStepEnrollSecondFactor
		}
		entry := forUser(auditEntry(request, "auth.login.password", audit.OutcomeSuccess, nil), user.ID, user.Username)
		return s.audit.RecordInTx(ctx, tx, entry)
	})
	return result, err
}

// VerifySecondFactor completes a login with a TOTP code or a recovery code.
// The password-only session is replaced by a new one, so its token can't be
// reused.
func (s *Service) VerifySecondFactor(ctx context.Context, session Session, input SecondFactorInput, request RequestInfo) (SignedIn, error) {
	if session.IsSignedIn() || !session.HasSecondFactor {
		return SignedIn{}, ErrWrongLoginStep
	}
	if err := s.checkRateLimit(ctx, session.Username, request); err != nil {
		return SignedIn{}, err
	}

	var signedIn SignedIn
	var codeValid bool
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		method, valid, remaining, err := s.checkSecondFactor(ctx, tx, session.UserID, input)
		if err != nil || !valid {
			return err
		}
		codeValid = true
		signedIn.UsedRecoveryCode = method == "recovery_code"
		signedIn.RemainingRecoveryCodes = remaining
		signedIn.SessionToken, err = s.replaceSession(ctx, tx, session, request)
		if err != nil {
			return err
		}
		if err := setUserLockedUntil(ctx, tx, session.UserID, nil); err != nil {
			return err
		}
		entry := forUser(auditEntry(request, "auth.login", audit.OutcomeSuccess, map[string]any{"method": method}), session.UserID, session.Username)
		return s.audit.RecordInTx(ctx, tx, entry)
	})
	if err != nil {
		return SignedIn{}, err
	}
	if !codeValid {
		return SignedIn{}, s.recordFailedAttempt(ctx, failedAttempt{user: User{ID: session.UserID, Username: session.Username},
			username: session.Username, request: request, action: "auth.login", reason: "invalid_code", err: ErrInvalidCode})
	}

	s.resetRateLimit(session.Username)
	return signedIn, nil
}

// checkSecondFactor checks whichever code the input carries. remaining is
// the number of unused recovery codes, after using one.
func (s *Service) checkSecondFactor(ctx context.Context, tx *sql.Tx, userID int64, input SecondFactorInput) (method string, valid bool, remaining int, err error) {
	if input.RecoveryCode != "" {
		valid, err = useRecoveryCode(ctx, tx, userID, hashRecoveryCode(input.RecoveryCode), s.now())
		if err != nil || !valid {
			return "recovery_code", false, 0, err
		}
		remaining, err = countUnusedRecoveryCodes(ctx, tx, userID)
		return "recovery_code", true, remaining, err
	}

	valid, err = s.checkTOTP(ctx, tx, userID, input.TOTPCode)
	return "totp", valid, 0, err
}

// checkTOTP verifies a TOTP code for userID and, if valid, records its step
// so it can't be used again.
func (s *Service) checkTOTP(ctx context.Context, tx *sql.Tx, userID int64, code string) (bool, error) {
	stored, err := s.loadTOTPSecret(ctx, tx, userID)
	if errors.Is(err, errNoTOTPSecret) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !stored.confirmed {
		return false, nil
	}

	step, valid := verifyTOTP(stored.secret, strings.TrimSpace(code), s.clock.Now(), stored.lastUsedStep)
	if !valid {
		return false, nil
	}
	return true, recordTOTPStep(ctx, tx, userID, step)
}

// replaceSession deletes session and starts a fully signed-in one for the
// same user, returning its token.
func (s *Service) replaceSession(ctx context.Context, tx *sql.Tx, session Session, request RequestInfo) (string, error) {
	if err := deleteSession(ctx, tx, session.ID); err != nil {
		return "", err
	}
	now := s.now()
	return s.startSession(ctx, tx, session.UserID, &now, request)
}

// findUserForLogin returns the user, or a User with no ID when the username
// doesn't exist. Login treats both the same, so the response doesn't reveal
// which usernames exist.
func (s *Service) findUserForLogin(ctx context.Context, username string) (User, error) {
	var user User
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		user, err = findUserByUsername(ctx, tx, username)
		if errors.Is(err, errUserNotFound) {
			user = User{}
			return nil
		}
		return err
	})
	return user, err
}

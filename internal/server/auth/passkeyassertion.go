// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// BeginPasskeyAssertion returns the options for the browser's
// navigator.credentials.get() call, as JSON. A password-only session uses
// it to finish signing in (SEC-04); a signed-in session uses it for a
// step-up check (SEC-07).
func (s *Service) BeginPasskeyAssertion(ctx context.Context, session Session, rp RelyingParty) (json.RawMessage, error) {
	if !session.HasPasskey {
		return nil, ErrWrongLoginStep
	}
	relyingParty, err := newWebAuthn(rp)
	if err != nil {
		return nil, err
	}

	var options json.RawMessage
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		user, err := loadWebAuthnUser(ctx, tx, session.UserID, session.Username)
		if err != nil {
			return err
		}
		assertion, data, err := relyingParty.BeginLogin(user)
		if err != nil {
			return fmt.Errorf("start passkey check: %w", err)
		}
		s.ceremonies.Start(session.ID, ceremonyAssertion, *data)
		options, err = json.Marshal(assertion)
		return err
	})
	return options, err
}

// FinishPasskeyAssertion checks the browser's signed answer. For a
// password-only session it completes sign-in and replaces the session token;
// for a signed-in session it restarts the step-up window. Failures count
// towards rate limiting like a wrong code.
func (s *Service) FinishPasskeyAssertion(ctx context.Context, session Session, rp RelyingParty, response []byte, request RequestInfo) (SignedIn, error) {
	if !session.HasPasskey {
		return SignedIn{}, ErrWrongLoginStep
	}
	if err := s.checkRateLimit(ctx, session.Username, request); err != nil {
		return SignedIn{}, err
	}

	var signedIn SignedIn
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		credential, err := s.validateAssertion(ctx, tx, session, rp, response)
		if err != nil {
			return err
		}
		if err := recordPasskeyUse(ctx, tx, credential, s.now()); err != nil {
			return err
		}
		signedIn, err = s.completePasskeyCheck(ctx, tx, session, request)
		return err
	})
	if errors.Is(err, ErrPasskeyFailed) {
		return SignedIn{}, s.recordFailedAttempt(ctx, failedAttempt{user: User{ID: session.UserID, Username: session.Username},
			username: session.Username, request: request, action: passkeyAction(session), reason: "passkey_failed", err: ErrPasskeyFailed})
	}
	if err != nil {
		return SignedIn{}, err
	}
	s.resetRateLimit(session.Username)
	return signedIn, nil
}

// validateAssertion checks the signature against the session's pending
// challenge and the user's stored passkeys. Any mismatch is
// ErrPasskeyFailed.
func (s *Service) validateAssertion(ctx context.Context, tx *sql.Tx, session Session, rp RelyingParty, response []byte) (*webauthn.Credential, error) {
	data, found := s.ceremonies.Finish(session.ID, ceremonyAssertion)
	if !found {
		return nil, ErrPasskeyFailed
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return nil, ErrPasskeyFailed
	}
	relyingParty, err := newWebAuthn(rp)
	if err != nil {
		return nil, err
	}
	user, err := loadWebAuthnUser(ctx, tx, session.UserID, session.Username)
	if err != nil {
		return nil, err
	}

	credential, err := relyingParty.ValidateLogin(user, data, parsed)
	// A clone warning means the signature counter went backwards: the
	// passkey may have been copied, so it isn't trusted.
	if err != nil || credential.Authenticator.CloneWarning {
		return nil, ErrPasskeyFailed
	}
	return credential, nil
}

// completePasskeyCheck signs a password-only session in, or restarts a
// signed-in session's step-up window, and audits it.
func (s *Service) completePasskeyCheck(ctx context.Context, tx *sql.Tx, session Session, request RequestInfo) (SignedIn, error) {
	entry := forUser(auditEntry(request, passkeyAction(session), audit.OutcomeSuccess, map[string]any{"method": "passkey"}),
		session.UserID, session.Username)
	if session.IsSignedIn() {
		if err := setSessionSecondFactorAt(ctx, tx, session.ID, s.now()); err != nil {
			return SignedIn{}, err
		}
		return SignedIn{}, s.audit.RecordInTx(ctx, tx, entry)
	}

	token, err := s.replaceSession(ctx, tx, session, request)
	if err != nil {
		return SignedIn{}, err
	}
	if err := setUserLockedUntil(ctx, tx, session.UserID, nil); err != nil {
		return SignedIn{}, err
	}
	return SignedIn{SessionToken: token}, s.audit.RecordInTx(ctx, tx, entry)
}

// passkeyAction is the audit action for a passkey check by session.
func passkeyAction(session Session) string {
	if session.IsSignedIn() {
		return "auth.step_up"
	}
	return "auth.login"
}

// ListPasskeys returns the signed-in user's passkeys.
func (s *Service) ListPasskeys(ctx context.Context, session Session) ([]Passkey, error) {
	if !session.IsSignedIn() {
		return nil, ErrNotAuthenticated
	}
	var passkeys []Passkey
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		passkeys, err = listPasskeys(ctx, tx, session.UserID)
		return err
	})
	return passkeys, err
}

// DeletePasskey removes one of the signed-in user's passkeys. It needs a
// recent second-factor check and, where 2FA is required, refuses to remove
// the last second factor.
func (s *Service) DeletePasskey(ctx context.Context, session Session, passkeyID int64, request RequestInfo) error {
	if err := s.RequireRecentSecondFactor(session); err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		deleted, err := deletePasskey(ctx, tx, session.UserID, passkeyID)
		if err != nil {
			return err
		}
		if !deleted {
			return ErrNotFound
		}
		remaining, err := hasSecondFactor(ctx, tx, session.UserID)
		if err != nil {
			return err
		}
		required, err := twoFactorRequired(ctx, tx)
		if err != nil {
			return err
		}
		if !remaining && required {
			return ErrLastSecondFactor
		}
		if !remaining {
			if err := replaceRecoveryCodes(ctx, tx, session.UserID, nil); err != nil {
				return err
			}
		}
		return s.audit.RecordInTx(ctx, tx, forUser(auditEntry(request, "auth.passkey.remove", audit.OutcomeSuccess,
			map[string]any{"passkeyId": passkeyID}), session.UserID, session.Username))
	})
}

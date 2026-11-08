// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// RelyingParty is the site passkeys belong to: the host name browsers bind
// them to, and the origin the browser reports.
type RelyingParty struct {
	// ID is the host name, without scheme or port, e.g. "sentinel.example.com".
	ID string
	// Origin is scheme, host and port, e.g. "https://sentinel.example.com".
	Origin string
}

// maxPasskeyNameLength keeps passkey names to something a list can show.
const maxPasskeyNameLength = 64

// Passkey errors. They are never reassigned.
var (
	// ErrPasskeyFailed: the browser's answer didn't check out, or the
	// request expired.
	ErrPasskeyFailed = errors.New("the passkey couldn't be verified; try again")
	// ErrLastSecondFactor: removing the passkey would leave no second factor.
	ErrLastSecondFactor = errors.New("this is your only second factor; add another before removing it")
)

func newWebAuthn(rp RelyingParty) (*webauthn.WebAuthn, error) {
	config, err := webauthn.New(&webauthn.Config{RPID: rp.ID, RPDisplayName: "Sentinel", RPOrigins: []string{rp.Origin}})
	if err != nil {
		return nil, fmt.Errorf("configure passkeys for %s: %w", rp.Origin, err)
	}
	return config, nil
}

// canRegisterPasskey reports whether session may add a passkey: either a
// user still choosing their first second factor (SEC-04), or a signed-in
// user who passed a second-factor check recently (SEC-07).
func (s *Service) canRegisterPasskey(session Session) error {
	if !session.IsSignedIn() {
		if session.HasSecondFactor {
			return ErrWrongLoginStep
		}
		return nil
	}
	return s.RequireRecentSecondFactor(session)
}

// BeginPasskeyRegistration returns the options for the browser's
// navigator.credentials.create() call, as JSON.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, session Session, rp RelyingParty) (json.RawMessage, error) {
	if err := s.canRegisterPasskey(session); err != nil {
		return nil, err
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
		creation, data, err := relyingParty.BeginRegistration(user,
			webauthn.WithExclusions(webauthn.Credentials(user.credentials).CredentialDescriptors()),
			webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred))
		if err != nil {
			return fmt.Errorf("start passkey registration: %w", err)
		}
		s.ceremonies.Start(session.ID, ceremonyRegistration, *data)
		options, err = json.Marshal(creation)
		return err
	})
	return options, err
}

// FinishPasskeyRegistration checks the browser's answer and saves the
// passkey under name. For a user enrolling their first second factor it
// also issues recovery codes and signs them in, like TOTP enrollment.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, session Session, rp RelyingParty, name string, response []byte, request RequestInfo) (SignedIn, error) {
	if err := s.canRegisterPasskey(session); err != nil {
		return SignedIn{}, err
	}
	name, err := validatePasskeyName(name)
	if err != nil {
		return SignedIn{}, err
	}
	data, found := s.ceremonies.Finish(session.ID, ceremonyRegistration)
	if !found {
		return SignedIn{}, ErrPasskeyFailed
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return SignedIn{}, ErrPasskeyFailed
	}

	var signedIn SignedIn
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		credential, err := s.createCredential(ctx, tx, session, rp, data, parsed)
		if err != nil {
			return err
		}
		if err := insertPasskey(ctx, tx, newPasskeyRow{userID: session.UserID, name: name, credential: credential, createdAt: s.now()}); err != nil {
			return err
		}
		if err := s.audit.RecordInTx(ctx, tx, forUser(auditEntry(request, "auth.passkey.add", audit.OutcomeSuccess,
			map[string]any{"name": name}), session.UserID, session.Username)); err != nil {
			return err
		}
		if session.IsSignedIn() {
			return nil
		}
		signedIn, err = s.finishPasskeyEnrollment(ctx, tx, session, request)
		return err
	})
	return signedIn, err
}

func (s *Service) createCredential(ctx context.Context, tx *sql.Tx, session Session, rp RelyingParty,
	data webauthn.SessionData, parsed *protocol.ParsedCredentialCreationData,
) (*webauthn.Credential, error) {
	relyingParty, err := newWebAuthn(rp)
	if err != nil {
		return nil, err
	}
	user, err := loadWebAuthnUser(ctx, tx, session.UserID, session.Username)
	if err != nil {
		return nil, err
	}
	credential, err := relyingParty.CreateCredential(user, data, parsed)
	if err != nil {
		return nil, ErrPasskeyFailed
	}
	return credential, nil
}

// finishPasskeyEnrollment completes sign-in for a user whose first second
// factor is a passkey.
func (s *Service) finishPasskeyEnrollment(ctx context.Context, tx *sql.Tx, session Session, request RequestInfo) (SignedIn, error) {
	codes, hashes := newRecoveryCodes()
	if err := replaceRecoveryCodes(ctx, tx, session.UserID, hashes); err != nil {
		return SignedIn{}, err
	}
	token, err := s.replaceSession(ctx, tx, session, request)
	if err != nil {
		return SignedIn{}, err
	}
	return SignedIn{SessionToken: token, RecoveryCodes: codes}, nil
}

func validatePasskeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	if utf8.RuneCountInString(name) > maxPasskeyNameLength || !utf8.ValidString(name) {
		return "", &ValidationError{Field: "name", Message: "use at most 64 characters"}
	}
	return name, nil
}

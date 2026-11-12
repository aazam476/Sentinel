// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"strings"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// BeginSetupInput is the first step of creating the first admin (SEC-02).
type BeginSetupInput struct {
	Username string
	Password string
}

// BeginSetupResult lets the admin add Sentinel to an authenticator app.
type BeginSetupResult struct {
	// SetupID identifies this setup in CompleteSetup.
	SetupID string
	TOTP    TOTPEnrollment
}

// CompleteSetupInput is the second step: proving the authenticator works.
type CompleteSetupInput struct {
	// SkipTwoFactor creates the admin without 2FA, which they can add later
	// from their account (ADR 0039). TOTPCode is then ignored.
	SkipTwoFactor bool
	SetupID       string
	TOTPCode      string
}

// SignedIn is the result of finishing a login, setup or enrollment.
type SignedIn struct {
	// SessionToken goes in the session cookie. It replaces any earlier token,
	// so a token seen before sign-in can't be used afterwards.
	SessionToken string
	// RecoveryCodes are shown to the user once, after setup or enrollment.
	RecoveryCodes []string
	// UsedRecoveryCode reports that the login was finished with a recovery
	// code, and RemainingRecoveryCodes how many unused ones are left.
	UsedRecoveryCode       bool
	RemainingRecoveryCodes int
}

// RemoveLeftoverSetupToken deletes the setup-token file servers before
// v0.1.0-beta.1 kept in the data directory: setup no longer asks for one
// (ADR 0037).
func (s *Service) RemoveLeftoverSetupToken() error {
	return removeSetupToken(s.setupTokenPath)
}

// SetupRequired reports whether the first admin still has to be created.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	var count int
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		count, err = countUsers(ctx, tx)
		return err
	})
	return count == 0, err
}

// BeginSetup checks the admin's chosen username and password, and returns
// a TOTP secret to enroll. Nothing is saved yet. Whoever opens the web UI
// first while no user exists may do this (ADR 0037).
func (s *Service) BeginSetup(ctx context.Context, input BeginSetupInput, request RequestInfo) (BeginSetupResult, error) {
	if err := s.checkSetupAllowed(ctx); err != nil {
		return BeginSetupResult{}, err
	}

	username, err := validateUsername(input.Username)
	if err != nil {
		return BeginSetupResult{}, err
	}
	if err := s.validatePassword(username, input.Password); err != nil {
		return BeginSetupResult{}, err
	}
	passwordHash, err := s.hasher.Hash(ctx, input.Password)
	if err != nil {
		return BeginSetupResult{}, err
	}
	enrollment, err := newTOTPEnrollment(username)
	if err != nil {
		return BeginSetupResult{}, err
	}

	setupID := s.pendingSetups.Add(pendingSetup{username: username, passwordHash: passwordHash, totpSecret: enrollment.Secret})
	return BeginSetupResult{SetupID: setupID, TOTP: enrollment}, nil
}

// CompleteSetup checks the TOTP code and then, in one transaction, creates
// the admin with confirmed TOTP and ten recovery codes, and signs them in.
// With SkipTwoFactor it creates the admin without either.
func (s *Service) CompleteSetup(ctx context.Context, input CompleteSetupInput, request RequestInfo) (SignedIn, error) {
	if err := s.checkSetupAllowed(ctx); err != nil {
		return SignedIn{}, err
	}
	setup, found := s.pendingSetups.Get(input.SetupID)
	if !found {
		return SignedIn{}, ErrSetupExpired
	}
	step, valid := verifyTOTP(setup.totpSecret, input.TOTPCode, s.clock.Now(), 0)
	if input.SkipTwoFactor {
		setup.totpSecret, step = "", 0
	} else if !valid {
		return SignedIn{}, ErrInvalidCode
	}

	var signedIn SignedIn
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		var err error
		signedIn, err = s.createFirstAdmin(ctx, tx, setup, step, request)
		return err
	})
	if err != nil {
		return SignedIn{}, err
	}

	s.pendingSetups.Clear()
	return signedIn, removeSetupToken(s.setupTokenPath)
}

// createFirstAdmin writes the admin, their 2FA and a signed-in session.
func (s *Service) createFirstAdmin(ctx context.Context, tx *sql.Tx, setup pendingSetup, step int64, request RequestInfo) (SignedIn, error) {
	// Checked again inside the transaction: two browsers could finish setup
	// at the same moment.
	count, err := countUsers(ctx, tx)
	if err != nil {
		return SignedIn{}, err
	}
	if count > 0 {
		return SignedIn{}, ErrSetupNotAvailable
	}

	now := s.now()
	userID, err := insertUser(ctx, tx, newUserRow{username: setup.username, passwordHash: setup.passwordHash, role: RoleAdmin, createdAt: now})
	if err != nil {
		return SignedIn{}, err
	}
	codes, err := s.enrollFirstAdmin(ctx, tx, userID, setup, step)
	if err != nil {
		return SignedIn{}, err
	}
	token, err := s.startSession(ctx, tx, userID, &now, request)
	if err != nil {
		return SignedIn{}, err
	}

	details := map[string]any{"role": RoleAdmin, "twoFactor": setup.totpSecret != ""}
	entry := forUser(auditEntry(request, "setup.complete", audit.OutcomeSuccess, details), userID, setup.username)
	if err := s.audit.RecordInTx(ctx, tx, entry); err != nil {
		return SignedIn{}, err
	}
	return SignedIn{SessionToken: token, RecoveryCodes: codes}, nil
}

// enrollFirstAdmin saves the admin's confirmed TOTP secret and returns
// their recovery codes; with no secret (2FA skipped) it saves nothing.
func (s *Service) enrollFirstAdmin(ctx context.Context, tx *sql.Tx, userID int64, setup pendingSetup, step int64) ([]string, error) {
	if setup.totpSecret == "" {
		required, err := twoFactorRequired(ctx, tx)
		if err != nil {
			return nil, err
		}
		if required {
			return nil, ErrTwoFactorRequired
		}
		return []string{}, nil
	}
	now := s.now()
	if err := s.saveTOTPSecret(ctx, tx, userID, setup.totpSecret, &now); err != nil {
		return nil, err
	}
	if err := recordTOTPStep(ctx, tx, userID, step); err != nil {
		return nil, err
	}
	codes, hashes := newRecoveryCodes()
	return codes, replaceRecoveryCodes(ctx, tx, userID, hashes)
}

// checkSetupAllowed allows setup only while no user exists.
func (s *Service) checkSetupAllowed(ctx context.Context) error {
	required, err := s.SetupRequired(ctx)
	if err != nil {
		return err
	}
	if !required {
		return ErrSetupNotAvailable
	}
	return nil
}

// startSession creates a session for userID and returns its token.
// secondFactorAt is nil for a session that has only passed the password.
func (s *Service) startSession(ctx context.Context, tx *sql.Tx, userID int64, secondFactorAt *int64, request RequestInfo) (string, error) {
	token, tokenHash := NewToken()
	_, err := insertSession(ctx, tx, newSessionRow{
		userID:         userID,
		tokenHash:      tokenHash,
		now:            s.now(),
		secondFactorAt: secondFactorAt,
		ip:             request.IP,
		userAgent:      truncate(request.UserAgent, maxUserAgentLength),
	})
	return token, err
}

// maxUserAgentLength keeps a hostile client from storing huge strings.
const maxUserAgentLength = 512

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	// Cutting at a byte limit can split a multi-byte character; drop the
	// broken remainder.
	return strings.ToValidUTF8(value[:limit], "")
}

func ipKey(ip string) string {
	return "ip:" + ip
}

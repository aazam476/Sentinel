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
	SetupToken string
	Username   string
	Password   string
}

// BeginSetupResult lets the admin add Sentinel to an authenticator app.
type BeginSetupResult struct {
	// SetupID identifies this setup in CompleteSetup.
	SetupID string
	TOTP    TOTPEnrollment
}

// CompleteSetupInput is the second step: proving the authenticator works.
type CompleteSetupInput struct {
	SetupToken string
	SetupID    string
	TOTPCode   string
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

// PrepareSetupToken makes sure a setup token exists while no user does and
// returns it, so the server can print it at startup (SEC-01). Once a user
// exists it removes any leftover token file and returns "".
func (s *Service) PrepareSetupToken(ctx context.Context) (string, error) {
	required, err := s.SetupRequired(ctx)
	if err != nil {
		return "", err
	}
	if !required {
		return "", removeSetupToken(s.setupTokenPath)
	}
	return readOrCreateSetupToken(s.setupTokenPath)
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

// BeginSetup checks the setup token and the admin's chosen username and
// password, and returns a TOTP secret to enroll. Nothing is saved yet.
func (s *Service) BeginSetup(ctx context.Context, input BeginSetupInput, request RequestInfo) (BeginSetupResult, error) {
	if err := s.checkSetupAllowed(ctx, input.SetupToken, request); err != nil {
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
// The setup token is deleted afterwards (SEC-01).
func (s *Service) CompleteSetup(ctx context.Context, input CompleteSetupInput, request RequestInfo) (SignedIn, error) {
	if err := s.checkSetupAllowed(ctx, input.SetupToken, request); err != nil {
		return SignedIn{}, err
	}
	setup, found := s.pendingSetups.Get(input.SetupID)
	if !found {
		return SignedIn{}, ErrSetupExpired
	}
	step, valid := verifyTOTP(setup.totpSecret, input.TOTPCode, s.clock.Now(), 0)
	if !valid {
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
	if err := s.saveTOTPSecret(ctx, tx, userID, setup.totpSecret, &now); err != nil {
		return SignedIn{}, err
	}
	if err := recordTOTPStep(ctx, tx, userID, step); err != nil {
		return SignedIn{}, err
	}
	codes, hashes := newRecoveryCodes()
	if err := replaceRecoveryCodes(ctx, tx, userID, hashes); err != nil {
		return SignedIn{}, err
	}
	token, err := s.startSession(ctx, tx, userID, &now, request)
	if err != nil {
		return SignedIn{}, err
	}

	entry := forUser(auditEntry(request, "setup.complete", audit.OutcomeSuccess, map[string]any{"role": RoleAdmin}), userID, setup.username)
	if err := s.audit.RecordInTx(ctx, tx, entry); err != nil {
		return SignedIn{}, err
	}
	return SignedIn{SessionToken: token, RecoveryCodes: codes}, nil
}

// checkSetupAllowed makes sure setup isn't done yet, the client isn't being
// rate limited, and the setup token is right. Wrong tokens are audited and
// count against the client's IP address.
func (s *Service) checkSetupAllowed(ctx context.Context, setupToken string, request RequestInfo) error {
	required, err := s.SetupRequired(ctx)
	if err != nil {
		return err
	}
	if !required {
		return ErrSetupNotAvailable
	}
	if wait := s.ipLimiter.RetryAfter(ipKey(request.IP)); wait > 0 {
		return &TooManyAttemptsError{RetryAfter: wait}
	}
	if setupTokenMatches(s.setupTokenPath, setupToken) {
		return nil
	}

	s.ipLimiter.RecordFailure(ipKey(request.IP))
	if err := s.audit.Record(ctx, auditEntry(request, "setup", audit.OutcomeFailure, map[string]any{"reason": "invalid_setup_token"})); err != nil {
		return err
	}
	return ErrInvalidSetupToken
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

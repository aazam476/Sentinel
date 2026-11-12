// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// ErrNotAnAdmin means reset-admin was pointed at a user who isn't an admin.
// It is never reassigned.
var ErrNotAnAdmin = errors.New("that user is not an admin")

// ResetAdmin is the local recovery for a locked-out admin (SEC-08), run by
// `sentinel-server reset-admin` on the server host. It gives the admin a new
// random password, removes their second factors (TOTP and passkeys) and
// recovery codes, unlocks
// and enables the account, and ends their sessions. Where 2FA is required
// they set it up again at the next sign-in (ADR 0039).
//
// An empty username picks the only admin; with several admins one must be
// named. The new password is returned for the operator to pass on.
func (s *Service) ResetAdmin(ctx context.Context, username string) (string, error) {
	newPassword := rand.Text()
	passwordHash, err := s.hasher.Hash(ctx, newPassword)
	if err != nil {
		return "", err
	}

	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		admin, err := findAdminToReset(ctx, tx, username)
		if err != nil {
			return err
		}
		if err := resetCredentials(ctx, tx, admin.ID, passwordHash); err != nil {
			return err
		}
		entry := forUser(audit.Entry{Action: "auth.admin.reset", Outcome: audit.OutcomeSuccess, IP: "local",
			Details: map[string]any{"via": "sentinel-server reset-admin"}}, admin.ID, admin.Username)
		return s.audit.RecordInTx(ctx, tx, entry)
	})
	if err != nil {
		return "", err
	}
	return newPassword, nil
}

// findAdminToReset returns the named admin, or the only admin when username
// is empty.
func findAdminToReset(ctx context.Context, tx *sql.Tx, username string) (User, error) {
	if username != "" {
		user, err := findUserByUsername(ctx, tx, username)
		if errors.Is(err, errUserNotFound) {
			return User{}, ErrNotFound
		}
		if err != nil {
			return User{}, err
		}
		if user.Role != RoleAdmin {
			return User{}, ErrNotAnAdmin
		}
		return user, nil
	}

	users, err := listUsers(ctx, tx)
	if err != nil {
		return User{}, err
	}
	var admins []User
	for _, user := range users {
		if user.Role == RoleAdmin {
			admins = append(admins, user)
		}
	}
	switch len(admins) {
	case 0:
		return User{}, ErrNotFound
	case 1:
		return admins[0], nil
	default:
		names := make([]string, 0, len(admins))
		for _, admin := range admins {
			names = append(names, admin.Username)
		}
		return User{}, fmt.Errorf("there are several admins (%s); choose one with --username", strings.Join(names, ", "))
	}
}

// resetCredentials replaces the password, unlocks and enables the account,
// and removes everything else that lets the user in.
func resetCredentials(ctx context.Context, tx *sql.Tx, userID int64, passwordHash string) error {
	_, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, locked_until = NULL, disabled_at = NULL WHERE id = ?`,
		passwordHash, userID)
	if err != nil {
		return fmt.Errorf("reset admin %d: %w", userID, err)
	}

	for _, statement := range []string{
		`DELETE FROM totp_secrets WHERE user_id = ?`,
		`DELETE FROM recovery_codes WHERE user_id = ?`,
		`DELETE FROM passkeys WHERE user_id = ?`,
		`DELETE FROM sessions WHERE user_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, userID); err != nil {
			return fmt.Errorf("reset admin %d: %w", userID, err)
		}
	}
	return nil
}

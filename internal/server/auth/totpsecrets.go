// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// totpSecret is a user's stored TOTP secret, decrypted.
type totpSecret struct {
	secret       string
	confirmed    bool
	lastUsedStep int64
}

// errNoTOTPSecret means the user has no TOTP secret. It is never reassigned.
var errNoTOTPSecret = errors.New("no TOTP secret")

// totpAssociatedData binds an encrypted TOTP secret to its user, so it
// can't be copied to another user's row (see internal/server/secrets).
func totpAssociatedData(userID int64) []byte {
	return []byte("totp-secret:user:" + strconv.FormatInt(userID, 10))
}

func (s *Service) loadTOTPSecret(ctx context.Context, tx *sql.Tx, userID int64) (totpSecret, error) {
	var sealed []byte
	var confirmedAt *int64
	var stored totpSecret
	err := tx.QueryRowContext(ctx, `SELECT secret_encrypted, confirmed_at, last_used_step FROM totp_secrets WHERE user_id = ?`,
		userID).Scan(&sealed, &confirmedAt, &stored.lastUsedStep)
	if errors.Is(err, sql.ErrNoRows) {
		return totpSecret{}, errNoTOTPSecret
	}
	if err != nil {
		return totpSecret{}, fmt.Errorf("read TOTP secret: %w", err)
	}

	plaintext, err := s.secrets.Decrypt(sealed, totpAssociatedData(userID))
	if err != nil {
		return totpSecret{}, fmt.Errorf("read TOTP secret of user %d: %w", userID, err)
	}
	stored.secret = string(plaintext)
	stored.confirmed = confirmedAt != nil
	return stored, nil
}

// saveTOTPSecret stores secret for userID, replacing any earlier one.
// confirmedAt is nil for a secret the user hasn't proven yet.
func (s *Service) saveTOTPSecret(ctx context.Context, tx *sql.Tx, userID int64, secret string, confirmedAt *int64) error {
	sealed, err := s.secrets.Encrypt([]byte(secret), totpAssociatedData(userID))
	if err != nil {
		return fmt.Errorf("encrypt TOTP secret: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO totp_secrets (user_id, secret_encrypted, confirmed_at, last_used_step)
		VALUES (?, ?, ?, 0)
		ON CONFLICT (user_id) DO UPDATE SET secret_encrypted = excluded.secret_encrypted,
			confirmed_at = excluded.confirmed_at, last_used_step = 0`,
		userID, sealed, confirmedAt)
	if err != nil {
		return fmt.Errorf("save TOTP secret: %w", err)
	}
	return nil
}

// hasConfirmedTOTP reports whether the user has finished setting up TOTP.
func hasConfirmedTOTP(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var confirmed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM totp_secrets WHERE user_id = ? AND confirmed_at IS NOT NULL)`,
		userID).Scan(&confirmed)
	if err != nil {
		return false, fmt.Errorf("check TOTP setup: %w", err)
	}
	return confirmed, nil
}

func confirmTOTPSecret(ctx context.Context, tx *sql.Tx, userID, confirmedAt, step int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE totp_secrets SET confirmed_at = ?, last_used_step = ? WHERE user_id = ?`,
		confirmedAt, step, userID)
	if err != nil {
		return fmt.Errorf("confirm TOTP secret: %w", err)
	}
	return nil
}

func recordTOTPStep(ctx context.Context, tx *sql.Tx, userID, step int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE totp_secrets SET last_used_step = ? WHERE user_id = ?`, step, userID); err != nil {
		return fmt.Errorf("record TOTP use: %w", err)
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// replaceRecoveryCodes deletes userID's recovery codes and stores the
// hashes of a new set.
func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64, hashes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("replace recovery codes: %w", err)
	}
	for _, hash := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, hash); err != nil {
			return fmt.Errorf("replace recovery codes: %w", err)
		}
	}
	return nil
}

// useRecoveryCode marks the unused code matching codeHash as used and
// reports whether there was one. Each code works once.
func useRecoveryCode(ctx context.Context, tx *sql.Tx, userID int64, codeHash string, now int64) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM recovery_codes WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
		userID, codeHash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check recovery code: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE recovery_codes SET used_at = ? WHERE id = ?`, now, id); err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	return true, nil
}

func countUnusedRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return count, nil
}

// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// ChangePassword sets a signed-in user's own password, given the current
// one. Wrong guesses count against the sign-in rate limit (SEC-05). The
// user's other sessions end, so whoever else knew the old password is
// signed out; this one stays.
func (s *Service) ChangePassword(ctx context.Context, session Session, current, replacement string, request RequestInfo) error {
	if !session.IsSignedIn() {
		return ErrNotAuthenticated
	}
	if err := s.checkRateLimit(ctx, session.Username, request); err != nil {
		return err
	}
	user, err := s.findUserForLogin(ctx, strings.TrimSpace(session.Username))
	if err != nil {
		return err
	}
	matches, err := s.hasher.Verify(ctx, current, user.passwordHash)
	if err != nil {
		return err
	}
	if !matches {
		return s.recordFailedAttempt(ctx, failedAttempt{user: user, username: session.Username, request: request,
			action: "auth.password.change", reason: "invalid_credentials", err: ErrInvalidCredentials})
	}
	s.resetRateLimit(session.Username)
	if err := s.validatePassword(session.Username, replacement); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(ctx, replacement)
	if err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		if err := setPassword(ctx, tx, session.UserID, hash); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id != ?`, session.UserID, session.ID); err != nil {
			return fmt.Errorf("end the user's other sessions: %w", err)
		}
		return s.audit.RecordInTx(ctx, tx, forUser(auditEntry(request, "auth.password.change", audit.OutcomeSuccess, nil),
			session.UserID, session.Username))
	})
}

// ResetPassword sets another user's password, for one who has forgotten
// theirs. Admins only, with a recent second factor (SEC-07). The user's
// sessions end and any lockout is lifted.
func (s *Service) ResetPassword(ctx context.Context, actor Session, userID int64, replacement string, request RequestInfo) error {
	if err := s.authorizeUserChange(actor); err != nil {
		return err
	}
	var target User
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		target, err = findUserByID(ctx, tx, userID)
		return err
	})
	if errors.Is(err, errUserNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := s.validatePassword(target.Username, replacement); err != nil {
		return err
	}
	// Hashing is slow; it happens before the write, not while holding it.
	hash, err := s.hasher.Hash(ctx, replacement)
	if err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		if err := setPassword(ctx, tx, target.ID, hash); err != nil {
			return err
		}
		if err := deleteSessionsForUser(ctx, tx, target.ID); err != nil {
			return err
		}
		return s.recordUserChange(ctx, tx, actor, request, "user.password.reset",
			map[string]any{"userId": target.ID, "username": target.Username})
	})
}

// setPassword replaces a password and lifts any lockout.
func setPassword(ctx context.Context, tx *sql.Tx, userID int64, hash string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, locked_until = NULL WHERE id = ?`, hash, userID); err != nil {
		return fmt.Errorf("set the password of user %d: %w", userID, err)
	}
	return nil
}

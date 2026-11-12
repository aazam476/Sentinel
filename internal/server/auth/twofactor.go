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

// settingRequireTwoFactor is the settings row that makes 2FA mandatory for
// every user (ADR 0039). Without it, 2FA is each user's choice.
const settingRequireTwoFactor = "auth.require_two_factor"

// TwoFactorRequired reports whether every user must use two-factor
// authentication (SEC-04). It is off until an admin turns it on.
func (s *Service) TwoFactorRequired(ctx context.Context) (bool, error) {
	var required bool
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		required, err = twoFactorRequired(ctx, tx)
		return err
	})
	return required, err
}

func twoFactorRequired(ctx context.Context, tx *sql.Tx) (bool, error) {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, settingRequireTwoFactor).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read the two-factor setting: %w", err)
	}
	return value == "true", nil
}

// SetTwoFactorRequired makes 2FA mandatory for everyone, or each user's
// choice again. Turning it on needs the admin to use 2FA themselves, and
// signs out everyone who doesn't: they set it up at their next sign-in.
func (s *Service) SetTwoFactorRequired(ctx context.Context, actor Session, required bool, request RequestInfo) error {
	if err := Authorize(actor, PermissionManageSettings); err != nil {
		return err
	}
	if err := s.RequireRecentSecondFactor(actor); err != nil {
		return err
	}
	if required && !actor.HasSecondFactor {
		return ErrOwnSecondFactorNeeded
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		value := "false"
		if required {
			value = "true"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json`, settingRequireTwoFactor, value); err != nil {
			return fmt.Errorf("save the two-factor setting: %w", err)
		}
		signedOut := int64(0)
		if required {
			var err error
			if signedOut, err = deleteSessionsWithoutSecondFactor(ctx, tx); err != nil {
				return err
			}
		}
		details := map[string]any{"required": required, "sessionsEnded": signedOut}
		return s.audit.RecordInTx(ctx, tx, forUser(auditEntry(request, "settings.require_two_factor", audit.OutcomeSuccess, details),
			actor.UserID, actor.Username))
	})
}

// deleteSessionsWithoutSecondFactor signs out every user who has no
// second factor, once 2FA becomes mandatory.
func deleteSessionsWithoutSecondFactor(ctx context.Context, tx *sql.Tx) (int64, error) {
	result, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id IN (SELECT u.id FROM users u WHERE
		NOT EXISTS (SELECT 1 FROM totp_secrets t WHERE t.user_id = u.id AND t.confirmed_at IS NOT NULL)
		AND NOT EXISTS (SELECT 1 FROM passkeys p WHERE p.user_id = u.id))`)
	if err != nil {
		return 0, fmt.Errorf("sign out users without 2FA: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sign out users without 2FA: %w", err)
	}
	return count, nil
}

// RemoveTOTP removes the signed-in user's authenticator app. Their
// recovery codes go with it unless a passkey remains. With 2FA required,
// it refuses to remove the last second factor.
func (s *Service) RemoveTOTP(ctx context.Context, session Session, request RequestInfo) error {
	if err := s.RequireRecentSecondFactor(session); err != nil {
		return err
	}
	if !session.HasTOTP {
		return ErrNotFound
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		required, err := twoFactorRequired(ctx, tx)
		if err != nil {
			return err
		}
		if required && !session.HasPasskey {
			return ErrLastSecondFactor
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM totp_secrets WHERE user_id = ?`, session.UserID); err != nil {
			return fmt.Errorf("remove the authenticator app: %w", err)
		}
		if !session.HasPasskey {
			if err := replaceRecoveryCodes(ctx, tx, session.UserID, nil); err != nil {
				return err
			}
		}
		return s.audit.RecordInTx(ctx, tx, forUser(auditEntry(request, "auth.totp.remove", audit.OutcomeSuccess, nil),
			session.UserID, session.Username))
	})
}

// StepUpWithPassword re-checks the password of a signed-in user who has no
// second factor, and restarts the step-up window (SEC-07). A user with a
// second factor must use it instead.
func (s *Service) StepUpWithPassword(ctx context.Context, session Session, password string, request RequestInfo) error {
	if !session.IsSignedIn() {
		return ErrNotAuthenticated
	}
	if session.HasSecondFactor {
		return ErrWrongLoginStep
	}
	if err := s.checkRateLimit(ctx, session.Username, request); err != nil {
		return err
	}
	user, err := s.findUserForLogin(ctx, strings.TrimSpace(session.Username))
	if err != nil {
		return err
	}
	matches, err := s.hasher.Verify(ctx, password, user.passwordHash)
	if err != nil {
		return err
	}
	if !matches {
		return s.recordFailedAttempt(ctx, failedAttempt{user: user, username: session.Username, request: request,
			action: "auth.step_up", reason: "invalid_credentials", err: ErrInvalidCredentials})
	}
	s.resetRateLimit(session.Username)
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		if err := setSessionSecondFactorAt(ctx, tx, session.ID, s.now()); err != nil {
			return err
		}
		return s.audit.RecordInTx(ctx, tx, forUser(auditEntry(request, "auth.step_up", audit.OutcomeSuccess,
			map[string]any{"method": "password"}), session.UserID, session.Username))
	})
}

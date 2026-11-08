// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Session is a signed-in browser (SEC-06).
type Session struct {
	ID         int64
	UserID     int64
	Username   string
	Role       Role
	CreatedAt  int64
	LastSeenAt int64
	// SecondFactorAt is when the user last passed a second-factor check in
	// this session, or nil while only the password step is done.
	SecondFactorAt *int64
	IP             string
	UserAgent      string
	// HasTOTP and HasPasskey report which second factors the user has set
	// up; HasSecondFactor is true when they have either.
	HasTOTP         bool
	HasPasskey      bool
	HasSecondFactor bool
	userDisabled    bool
}

// IsSignedIn reports whether the session has passed both login steps.
func (s Session) IsSignedIn() bool {
	return s.SecondFactorAt != nil
}

// errSessionNotFound means no session matched. It is never reassigned.
var errSessionNotFound = errors.New("session not found")

const selectSession = `SELECT s.id, s.user_id, u.username, u.role, s.created_at, s.last_seen_at, s.last_2fa_at,
	s.ip, s.user_agent, u.disabled_at IS NOT NULL,
	EXISTS (SELECT 1 FROM totp_secrets t WHERE t.user_id = u.id AND t.confirmed_at IS NOT NULL),
	EXISTS (SELECT 1 FROM passkeys p WHERE p.user_id = u.id)
	FROM sessions s JOIN users u ON u.id = s.user_id`

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var session Session
	var role string
	err := row.Scan(&session.ID, &session.UserID, &session.Username, &role, &session.CreatedAt, &session.LastSeenAt,
		&session.SecondFactorAt, &session.IP, &session.UserAgent, &session.userDisabled, &session.HasTOTP, &session.HasPasskey)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, errSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	session.Role = Role(role)
	session.HasSecondFactor = session.HasTOTP || session.HasPasskey
	return session, nil
}

func findSessionByTokenHash(ctx context.Context, tx *sql.Tx, tokenHash string) (Session, error) {
	return scanSession(tx.QueryRowContext(ctx, selectSession+` WHERE s.token_hash = ?`, tokenHash))
}

func findSessionByID(ctx context.Context, tx *sql.Tx, id int64) (Session, error) {
	return scanSession(tx.QueryRowContext(ctx, selectSession+` WHERE s.id = ?`, id))
}

func listSessionsForUser(ctx context.Context, tx *sql.Tx, userID int64) ([]Session, error) {
	rows, err := tx.QueryContext(ctx, selectSession+` WHERE s.user_id = ? ORDER BY s.last_seen_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }() // a close error after reading is not actionable

	sessions := []Session{}
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

// newSessionRow is what insertSession stores.
type newSessionRow struct {
	userID         int64
	tokenHash      string
	now            int64
	secondFactorAt *int64
	ip             string
	userAgent      string
}

func insertSession(ctx context.Context, tx *sql.Tx, row newSessionRow) (int64, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO sessions
		(user_id, token_hash, created_at, last_seen_at, last_2fa_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.userID, row.tokenHash, row.now, row.now, row.secondFactorAt, row.ip, row.userAgent)
	if err != nil {
		return 0, fmt.Errorf("create session: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

func touchSession(ctx context.Context, tx *sql.Tx, id, now int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, now, id); err != nil {
		return fmt.Errorf("update session %d: %w", id, err)
	}
	return nil
}

func setSessionSecondFactorAt(ctx context.Context, tx *sql.Tx, id, at int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_2fa_at = ? WHERE id = ?`, at, id); err != nil {
		return fmt.Errorf("update session %d: %w", id, err)
	}
	return nil
}

func deleteSession(ctx context.Context, tx *sql.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session %d: %w", id, err)
	}
	return nil
}

func deleteSessionsForUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete sessions of user %d: %w", userID, err)
	}
	return nil
}

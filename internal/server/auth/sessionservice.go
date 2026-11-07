// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// Session lifetimes (SEC-06).
const (
	// IdleTimeout ends a session nobody has used for this long.
	IdleTimeout = 12 * time.Hour
	// AbsoluteTimeout ends every session this long after sign-in, used or not.
	AbsoluteTimeout = 7 * 24 * time.Hour
	// PasswordStepTimeout ends a session that passed the password step but
	// never completed the second factor.
	PasswordStepTimeout = 10 * time.Minute
	// touchInterval limits how often a session's last-seen time is written,
	// so browsing doesn't turn every request into a database write.
	touchInterval = time.Minute
)

// SessionSummary is what a user sees in their list of sessions.
type SessionSummary struct {
	ID         int64  `json:"id"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	IP         string `json:"ip"`
	UserAgent  string `json:"userAgent"`
	// Current marks the session making the request.
	Current bool `json:"current"`
}

// Authenticate returns the session for a token from a session cookie. It
// fails with ErrNotAuthenticated for an unknown, expired or revoked session,
// or one whose user has been disabled. Expired sessions are deleted.
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrNotAuthenticated
	}

	var session Session
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		session, err = findSessionByTokenHash(ctx, tx, hashToken(token))
		return err
	})
	if errors.Is(err, errSessionNotFound) {
		return Session{}, ErrNotAuthenticated
	}
	if err != nil {
		return Session{}, err
	}

	now := s.clock.Now()
	if !s.sessionIsValid(session, now) {
		return Session{}, errors.Join(ErrNotAuthenticated, s.deleteSessionByID(ctx, session.ID))
	}
	if now.Sub(time.UnixMilli(session.LastSeenAt)) >= touchInterval {
		if err := s.touch(ctx, session.ID, now.UnixMilli()); err != nil {
			return Session{}, err
		}
		session.LastSeenAt = now.UnixMilli()
	}
	return session, nil
}

// sessionIsValid applies the session timeouts.
func (s *Service) sessionIsValid(session Session, now time.Time) bool {
	createdAt := time.UnixMilli(session.CreatedAt)
	switch {
	case session.userDisabled:
		return false
	case !session.IsSignedIn() && now.Sub(createdAt) > PasswordStepTimeout:
		return false
	case now.Sub(createdAt) > AbsoluteTimeout:
		return false
	case now.Sub(time.UnixMilli(session.LastSeenAt)) > IdleTimeout:
		return false
	default:
		return true
	}
}

func (s *Service) touch(ctx context.Context, sessionID, now int64) error {
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		return touchSession(ctx, tx, sessionID, now)
	})
}

func (s *Service) deleteSessionByID(ctx context.Context, sessionID int64) error {
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		return deleteSession(ctx, tx, sessionID)
	})
}

// Logout ends session.
func (s *Service) Logout(ctx context.Context, session Session, request RequestInfo) error {
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		if err := deleteSession(ctx, tx, session.ID); err != nil {
			return err
		}
		entry := forUser(auditEntry(request, "auth.logout", audit.OutcomeSuccess, nil), session.UserID, session.Username)
		return s.audit.RecordInTx(ctx, tx, entry)
	})
}

// ListSessions returns the signed-in user's sessions that are still valid,
// most recently used first.
func (s *Service) ListSessions(ctx context.Context, session Session) ([]SessionSummary, error) {
	var sessions []Session
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		sessions, err = listSessionsForUser(ctx, tx, session.UserID)
		return err
	})
	if err != nil {
		return nil, err
	}

	now := s.clock.Now()
	summaries := []SessionSummary{}
	for _, other := range sessions {
		if !s.sessionIsValid(other, now) {
			continue
		}
		summaries = append(summaries, SessionSummary{
			ID: other.ID, CreatedAt: other.CreatedAt, LastSeenAt: other.LastSeenAt,
			IP: other.IP, UserAgent: other.UserAgent, Current: other.ID == session.ID,
		})
	}
	return summaries, nil
}

// RevokeSession ends one of the signed-in user's own sessions (SEC-06).
// Another user's session ID gives ErrNotFound, revealing nothing about it.
func (s *Service) RevokeSession(ctx context.Context, session Session, sessionID int64, request RequestInfo) error {
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		target, err := findSessionByID(ctx, tx, sessionID)
		if errors.Is(err, errSessionNotFound) || (err == nil && target.UserID != session.UserID) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := deleteSession(ctx, tx, sessionID); err != nil {
			return err
		}
		entry := forUser(auditEntry(request, "auth.session.revoke", audit.OutcomeSuccess, map[string]any{"sessionId": sessionID}),
			session.UserID, session.Username)
		return s.audit.RecordInTx(ctx, tx, entry)
	})
}

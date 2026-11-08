// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// maxAuditedUsernameLength caps a username typed at the login form before
// it goes into the audit log. Real usernames are at most 64 characters; a
// longer one can't exist, and storing whatever an attacker sends would let
// them fill the log with huge entries.
const maxAuditedUsernameLength = 128

// failedAttempt describes a failed login step for recordFailedAttempt.
type failedAttempt struct {
	// user is the account, or a User with ID 0 when the username doesn't exist.
	user     User
	username string
	request  RequestInfo
	action   string
	reason   string
	// err is returned to the caller after recording, unless a lock applies.
	err error
}

// checkRateLimit refuses an attempt while the client's IP address or the
// username is in backoff or locked (SEC-05). Refusals are audited.
func (s *Service) checkRateLimit(ctx context.Context, username string, request RequestInfo) error {
	wait := max(s.ipLimiter.RetryAfter(ipKey(request.IP)), s.usernameLimit.RetryAfter(usernameKey(username)))
	if wait == 0 {
		return nil
	}

	err := &TooManyAttemptsError{RetryAfter: wait}
	entry := auditEntry(request, "auth.login", audit.OutcomeDenied, map[string]any{"reason": "rate_limited"})
	entry.Username = truncate(username, maxAuditedUsernameLength)
	if recordErr := s.audit.Record(ctx, entry); recordErr != nil {
		return recordErr
	}
	return err
}

// checkNotLocked refuses a user whose account was locked by repeated
// failures. The lock is stored in the database, so it survives a restart.
func (s *Service) checkNotLocked(user User) error {
	if user.lockedUntil == nil {
		return nil
	}
	remaining := time.UnixMilli(*user.lockedUntil).Sub(s.clock.Now())
	if remaining <= 0 {
		return nil
	}
	return &TooManyAttemptsError{RetryAfter: remaining}
}

// auditRefusal records a login refused because the account is locked, and
// returns err.
func (s *Service) auditRefusal(ctx context.Context, user User, username string, request RequestInfo, err error) error {
	entry := forUser(auditEntry(request, "auth.login", audit.OutcomeDenied, map[string]any{"reason": "account_locked"}), user.ID, username)
	if recordErr := s.audit.Record(ctx, entry); recordErr != nil {
		return recordErr
	}
	return err
}

// recordFailedAttempt counts a failure against the IP address and username,
// locks the account when it reaches the limit, and audits the failure
// (SEC-05: every failure is logged).
func (s *Service) recordFailedAttempt(ctx context.Context, attempt failedAttempt) error {
	s.ipLimiter.RecordFailure(ipKey(attempt.request.IP))
	locked := s.usernameLimit.RecordFailure(usernameKey(attempt.username))

	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		entry := auditEntry(attempt.request, attempt.action, audit.OutcomeFailure, map[string]any{"reason": attempt.reason})
		entry.Username = truncate(attempt.username, maxAuditedUsernameLength)
		if attempt.user.ID != 0 {
			entry = forUser(entry, attempt.user.ID, attempt.username)
		}
		if err := s.audit.RecordInTx(ctx, tx, entry); err != nil {
			return err
		}
		if locked && attempt.user.ID != 0 {
			return s.lockAccount(ctx, tx, attempt)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if locked {
		return &TooManyAttemptsError{RetryAfter: lockoutDuration}
	}
	return attempt.err
}

func (s *Service) lockAccount(ctx context.Context, tx *sql.Tx, attempt failedAttempt) error {
	lockedUntil := s.clock.Now().Add(lockoutDuration).UnixMilli()
	if err := setUserLockedUntil(ctx, tx, attempt.user.ID, &lockedUntil); err != nil {
		return err
	}
	entry := forUser(auditEntry(attempt.request, "auth.account.lock", audit.OutcomeSuccess,
		map[string]any{"reason": "too_many_failures", "minutes": int(lockoutDuration / time.Minute)}), attempt.user.ID, attempt.username)
	return s.audit.RecordInTx(ctx, tx, entry)
}

// resetRateLimit clears the username's failures after a complete login. The
// IP address keeps its count: otherwise an attacker could clear it by
// signing in to an account of their own between guesses.
func (s *Service) resetRateLimit(username string) {
	s.usernameLimit.Reset(usernameKey(username))
}

// usernameKey is case-insensitive, like usernames. It is capped like the
// audited name, so huge typed usernames can't bloat the limiter's memory.
func usernameKey(username string) string {
	return "user:" + strings.ToLower(truncate(username, maxAuditedUsernameLength))
}

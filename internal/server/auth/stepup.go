// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// StepUpWindow is how recent the last second-factor check must be for a
// sensitive action (SEC-07): opening a terminal, starting a desktop session,
// running a script on more than one machine, or changing users and roles.
const StepUpWindow = 15 * time.Minute

// RequireRecentSecondFactor returns ErrStepUpRequired unless session passed
// a second-factor check within StepUpWindow.
func (s *Service) RequireRecentSecondFactor(session Session) error {
	if !session.IsSignedIn() {
		return ErrNotAuthenticated
	}
	lastCheck := time.UnixMilli(*session.SecondFactorAt)
	if s.clock.Now().Sub(lastCheck) > StepUpWindow {
		return ErrStepUpRequired
	}
	return nil
}

// StepUp re-checks a signed-in user's TOTP code and, if correct, restarts
// the step-up window for the session. Recovery codes aren't accepted here:
// they are for getting back in, not for routine confirmations.
func (s *Service) StepUp(ctx context.Context, session Session, code string, request RequestInfo) error {
	if !session.IsSignedIn() {
		return ErrNotAuthenticated
	}
	if err := s.checkRateLimit(ctx, session.Username, request); err != nil {
		return err
	}

	var codeValid bool
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		valid, err := s.checkTOTP(ctx, tx, session.UserID, code)
		if err != nil || !valid {
			return err
		}
		codeValid = true
		if err := setSessionSecondFactorAt(ctx, tx, session.ID, s.now()); err != nil {
			return err
		}
		entry := forUser(auditEntry(request, "auth.step_up", audit.OutcomeSuccess, nil), session.UserID, session.Username)
		return s.audit.RecordInTx(ctx, tx, entry)
	})
	if err != nil {
		return err
	}
	if !codeValid {
		return s.recordFailedAttempt(ctx, failedAttempt{user: User{ID: session.UserID, Username: session.Username},
			username: session.Username, request: request, action: "auth.step_up", reason: "invalid_code", err: ErrInvalidCode})
	}
	s.resetRateLimit(session.Username)
	return nil
}

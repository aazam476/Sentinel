// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// Nothing a person does makes a script run on more than one machine unless
// they passed a second-factor check in the last 15 minutes (SEC-07): not
// running it now, not saving a schedule that will, and not changing a
// script such a schedule runs. A script on one machine needs no check, as
// in the specification. See docs/decisions/0043-step-up-for-scripts.md.

// errNoStepUp means the service was built without a StepUp. It is never
// reassigned.
var errNoStepUp = errors.New("scripts: no second-factor check is configured, so scripts can't run on more than one machine")

// requireStepUp returns auth.ErrStepUpRequired unless actor passed a
// second-factor check recently.
func (s *Service) requireStepUp(actor auth.Session) error {
	if s.stepUp == nil {
		// Refused, not skipped: a server wired without the check must not
		// quietly run scripts on a whole fleet.
		return errNoStepUp
	}
	return s.stepUp.RequireRecentSecondFactor(actor)
}

// checkScriptChange asks for the check before a script changes that a
// schedule runs on more than one machine. A new script (ID 0) has no
// schedules yet.
func (s *Service) checkScriptChange(ctx context.Context, actor auth.Session, scriptID int64) error {
	if scriptID == 0 {
		return nil
	}
	onMany, err := s.scheduledOnMany(ctx, scriptID)
	if err != nil || !onMany {
		return err
	}
	return s.requireStepUp(actor)
}

// machineIDs are the machines a target of kind "agent" or "agents" names,
// each once.
func (t Target) machineIDs() []string {
	var ids []string
	for id := range strings.SplitSeq(t.Value, ",") {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// reachesMany reports whether a schedule with this target can run on more
// than one machine. A tag can, whatever it matches today: machines are
// tagged later without anyone looking at the schedule again.
func (t Target) reachesMany() bool {
	return t.Kind == "tag" || len(t.machineIDs()) > 1
}

// scheduledOnMany reports whether a schedule that is switched on, and can
// reach more than one machine, runs the script.
func (s *Service) scheduledOnMany(ctx context.Context, scriptID int64) (bool, error) {
	found := false
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT target_kind, target_value FROM schedules WHERE script_id = ? AND enabled = 1`, scriptID)
		if err != nil {
			return fmt.Errorf("find the script's schedules: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var target Target
			if err := rows.Scan(&target.Kind, &target.Value); err != nil {
				return fmt.Errorf("read a schedule's target: %w", err)
			}
			found = found || target.reachesMany()
		}
		return rows.Err()
	})
	return found, err
}

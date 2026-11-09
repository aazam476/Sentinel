// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Schedule runs a script on a target at the times its cron gives.
type Schedule struct {
	ID         int64
	ScriptID   int64
	ScriptName string
	Cron       string
	Timezone   string
	Target     Target
	Enabled    bool
	// NextRunAt and LastRunAt are UTC Unix milliseconds, or nil.
	NextRunAt *int64
	LastRunAt *int64
}

// validate checks a schedule and works out when it runs next.
func (s *Schedule) validate(now time.Time) (*int64, error) {
	cron, err := ParseCron(s.Cron)
	if err != nil {
		return nil, &InvalidError{"cron", strings.TrimPrefix(err.Error(), ErrCron.Error()+": ")}
	}
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	location, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, &InvalidError{"timezone", "choose a time zone such as Europe/Berlin"}
	}
	s.Target.Value = strings.TrimSpace(s.Target.Value)
	if (s.Target.Kind != "agent" && s.Target.Kind != "tag" && s.Target.Kind != "agents") || s.Target.Value == "" {
		return nil, &InvalidError{"target", "choose a machine, a tag or several machines"}
	}
	if !s.Enabled {
		return nil, nil
	}
	next := cron.Next(now, location)
	if next.IsZero() {
		return nil, &InvalidError{"cron", "that schedule never comes round"}
	}
	millis := next.UnixMilli()
	return &millis, nil
}

const scheduleColumns = `s.id, s.script_id, c.name, s.cron, s.timezone, s.target_kind, s.target_value, s.enabled, s.next_run_at, s.last_run_at`

func scanSchedule(row scanner) (Schedule, error) {
	var sc Schedule
	err := row.Scan(&sc.ID, &sc.ScriptID, &sc.ScriptName, &sc.Cron, &sc.Timezone, &sc.Target.Kind, &sc.Target.Value, &sc.Enabled,
		&sc.NextRunAt, &sc.LastRunAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("read a schedule: %w", err)
	}
	return sc, nil
}

// Schedules lists every schedule, soonest first.
func (s *Service) Schedules(ctx context.Context) ([]Schedule, error) {
	var list []Schedule
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM schedules s JOIN scripts c ON c.id = s.script_id
			ORDER BY s.next_run_at IS NULL, s.next_run_at, s.id`)
		if err != nil {
			return fmt.Errorf("list schedules: %w", err)
		}
		defer func() { _ = rows.Close() }()
		list = []Schedule{}
		for rows.Next() {
			schedule, err := scanSchedule(rows)
			if err != nil {
				return err
			}
			list = append(list, schedule)
		}
		return rows.Err()
	})
	return list, err
}

// SaveSchedule adds a schedule (ID 0) or changes one, and audits it.
func (s *Service) SaveSchedule(ctx context.Context, actor auth.Session, request auth.RequestInfo, schedule Schedule) (Schedule, error) {
	if err := auth.Authorize(actor, auth.PermissionRunScripts); err != nil {
		return Schedule{}, err
	}
	next, err := schedule.validate(s.clock.Now())
	if err != nil {
		return Schedule{}, err
	}
	if _, err := s.GetScript(ctx, schedule.ScriptID); err != nil {
		return Schedule{}, &InvalidError{"scriptId", "choose a script from the library"}
	}
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		if schedule.ID == 0 {
			result, err := tx.ExecContext(ctx, `INSERT INTO schedules (script_id, cron, timezone, target_kind, target_value, enabled, created_by, created_at, next_run_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, schedule.ScriptID, schedule.Cron, schedule.Timezone, schedule.Target.Kind,
				schedule.Target.Value, schedule.Enabled, actor.UserID, s.clock.Now().UnixMilli(), next)
			if err == nil {
				schedule.ID, err = result.LastInsertId()
			}
			if err != nil {
				return fmt.Errorf("add the schedule: %w", err)
			}
		} else if err := updateSchedule(ctx, tx, schedule, next); err != nil {
			return err
		}
		schedule.NextRunAt = next
		return s.audit.RecordInTx(ctx, tx, userEntry(actor, request, "schedule.save", map[string]any{
			"schedule": schedule.ID, "script": schedule.ScriptID, "cron": schedule.Cron, "timezone": schedule.Timezone,
			"target": schedule.Target.String(), "enabled": schedule.Enabled,
		}))
	})
	return schedule, err
}

func updateSchedule(ctx context.Context, tx *sql.Tx, schedule Schedule, next *int64) error {
	result, err := tx.ExecContext(ctx, `UPDATE schedules SET script_id = ?, cron = ?, timezone = ?, target_kind = ?, target_value = ?,
		enabled = ?, next_run_at = ? WHERE id = ?`, schedule.ScriptID, schedule.Cron, schedule.Timezone, schedule.Target.Kind,
		schedule.Target.Value, schedule.Enabled, next, schedule.ID)
	if err != nil {
		return fmt.Errorf("change the schedule: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSchedule removes a schedule; the runs it started stay.
func (s *Service) DeleteSchedule(ctx context.Context, actor auth.Session, request auth.RequestInfo, id int64) error {
	if err := auth.Authorize(actor, auth.PermissionRunScripts); err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete the schedule: %w", err)
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return ErrNotFound
		}
		return s.audit.RecordInTx(ctx, tx, userEntry(actor, request, "schedule.delete", map[string]any{"schedule": id}))
	})
}

// RunDue starts every enabled schedule whose time has come and works out
// its next time. The scheduler calls it every minute. A schedule whose
// script can't start (no machine matches its tag, say) is logged and
// waits for its next time.
func (s *Service) RunDue(ctx context.Context) error {
	now := s.clock.Now()
	var due []Schedule
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM schedules s JOIN scripts c ON c.id = s.script_id
			WHERE s.enabled = 1 AND s.next_run_at <= ? ORDER BY s.next_run_at`, now.UnixMilli())
		if err != nil {
			return fmt.Errorf("find due schedules: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			schedule, err := scanSchedule(rows)
			if err != nil {
				return err
			}
			due = append(due, schedule)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, schedule := range due {
		s.runScheduled(ctx, schedule, now)
	}
	return nil
}

func (s *Service) runScheduled(ctx context.Context, schedule Schedule, now time.Time) {
	scheduleID := schedule.ID
	entry := audit.Entry{Action: "script.run", Outcome: audit.OutcomeSuccess}
	if _, err := s.start(ctx, schedule.ScriptID, schedule.Target, starter{scheduleID: &scheduleID, entry: entry}); err != nil {
		s.logger.Warn("a scheduled script couldn't start", "schedule", schedule.ID, "script", schedule.ScriptName, "error", err)
	}
	next, err := schedule.validate(now)
	if err != nil {
		next = nil // it can no longer run; it stays, disabled in effect, for someone to fix
	}
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE schedules SET last_run_at = ?, next_run_at = ? WHERE id = ?`, now.UnixMilli(), next, schedule.ID)
		return err
	})
	if err != nil {
		s.logger.Error("could not set a schedule's next run", "schedule", schedule.ID, "error", err)
	}
}

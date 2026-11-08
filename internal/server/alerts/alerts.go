// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Database runs transactions (internal/server/store).
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// States.
const (
	Firing   = "firing"
	Resolved = "resolved"
)

// Alert is one condition on one machine, firing or resolved.
type Alert struct {
	ID       int64
	AgentID  int64
	Kind     string
	Subject  string
	Severity string
	Message  string
	State    string
	// StartedAt and ResolvedAt are UTC Unix milliseconds; ResolvedAt is 0
	// while firing.
	StartedAt  int64
	ResolvedAt int64
}

// Service keeps alerts.
type Service struct {
	database Database
	clock    Clock
}

// NewService returns a Service.
func NewService(database Database, clock Clock) *Service {
	return &Service{database: database, clock: clock}
}

// Reconcile makes a machine's firing alerts of the given kinds match
// conditions: new conditions start firing, ones that hold keep firing
// (with the newest message) and the rest are resolved. It returns whether
// anything changed.
func (s *Service) Reconcile(ctx context.Context, agentID int64, kinds []string, conditions []Condition) (bool, error) {
	now := s.clock.Now().UnixMilli()
	changed := false
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		firing, err := firingIn(ctx, tx, agentID, kinds)
		if err != nil {
			return err
		}
		if changed, err = raise(ctx, tx, agentID, firing, conditions, now); err != nil {
			return err
		}
		for _, alert := range firing {
			changed = true
			if _, err := tx.ExecContext(ctx, `UPDATE alerts SET state = 'resolved', resolved_at = ? WHERE id = ?`, now, alert.ID); err != nil {
				return fmt.Errorf("resolve alert %d: %w", alert.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("reconcile alerts: %w", err)
	}
	return changed, nil
}

// raise starts or updates an alert for each condition, removing from
// firing the alerts that still hold, so only those to resolve are left.
func raise(ctx context.Context, tx *sql.Tx, agentID int64, firing map[string]Alert, conditions []Condition, now int64) (bool, error) {
	changed := false
	for _, condition := range conditions {
		key := condition.Kind + "\x00" + condition.Subject
		alert, found := firing[key]
		delete(firing, key)
		if found && alert.Message == condition.Message && alert.Severity == condition.Severity {
			continue
		}
		changed = true
		if err := upsert(ctx, tx, agentID, alert, found, condition, now); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func upsert(ctx context.Context, tx *sql.Tx, agentID int64, alert Alert, found bool, condition Condition, now int64) error {
	if found {
		_, err := tx.ExecContext(ctx, `UPDATE alerts SET severity = ?, message = ? WHERE id = ?`,
			condition.Severity, condition.Message, alert.ID)
		if err != nil {
			return fmt.Errorf("update alert %d: %w", alert.ID, err)
		}
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO alerts (agent_id, kind, subject, severity, message, state, started_at)
		VALUES (?, ?, ?, ?, ?, 'firing', ?)`, agentID, condition.Kind, condition.Subject, condition.Severity, condition.Message, now)
	if err != nil {
		return fmt.Errorf("raise alert %s for %s: %w", condition.Kind, condition.Subject, err)
	}
	return nil
}

// firingIn returns a machine's firing alerts of the given kinds, keyed by
// kind and subject.
func firingIn(ctx context.Context, tx *sql.Tx, agentID int64, kinds []string) (map[string]Alert, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(kinds)), ", ")
	args := []any{agentID}
	for _, kind := range kinds {
		args = append(args, kind)
	}
	query := `SELECT ` + alertColumns + ` FROM alerts WHERE agent_id = ? AND state = 'firing' AND kind IN (` + placeholders + `)`
	alerts, err := queryAlerts(ctx, tx, query, args...)
	if err != nil {
		return nil, err
	}
	result := map[string]Alert{}
	for _, alert := range alerts {
		result[alert.Kind+"\x00"+alert.Subject] = alert
	}
	return result, nil
}

const alertColumns = `id, agent_id, kind, subject, severity, message, state, started_at, COALESCE(resolved_at, 0)`

func queryAlerts(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Alert, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	alerts := []Alert{}
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.AgentID, &a.Kind, &a.Subject, &a.Severity, &a.Message, &a.State, &a.StartedAt, &a.ResolvedAt); err != nil {
			return nil, fmt.Errorf("read an alert: %w", err)
		}
		alerts = append(alerts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read alerts: %w", err)
	}
	return alerts, nil
}

// ForAgent returns a machine's firing alerts, most severe and newest
// first, then up to limit resolved ones, newest first.
func (s *Service) ForAgent(ctx context.Context, agentID int64, limit int) ([]Alert, error) {
	var alerts []Alert
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		firing, err := queryAlerts(ctx, tx, `SELECT `+alertColumns+` FROM alerts WHERE agent_id = ? AND state = 'firing'
			ORDER BY severity = 'critical' DESC, started_at DESC, id DESC`, agentID)
		if err != nil {
			return err
		}
		resolved, err := queryAlerts(ctx, tx, `SELECT `+alertColumns+` FROM alerts WHERE agent_id = ? AND state = 'resolved'
			ORDER BY resolved_at DESC, id DESC LIMIT ?`, agentID, limit)
		alerts = slices.Concat(firing, resolved)
		return err
	})
	return alerts, err
}

// FiringCounts returns how many alerts are firing on each machine that
// has any.
func (s *Service) FiringCounts(ctx context.Context) (map[int64]int, error) {
	counts := map[int64]int{}
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT agent_id, COUNT(*) FROM alerts WHERE state = 'firing' GROUP BY agent_id`)
		if err != nil {
			return fmt.Errorf("count alerts: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var agentID int64
			var count int
			if err := rows.Scan(&agentID, &count); err != nil {
				return fmt.Errorf("count alerts: %w", err)
			}
			counts[agentID] = count
		}
		return rows.Err()
	})
	return counts, err
}

// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Database runs transactions (internal/server/store).
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// AuditLog records changes in the same transaction as the change.
type AuditLog interface {
	RecordInTx(ctx context.Context, tx *sql.Tx, entry audit.Entry) error
}

// Sealer encrypts and decrypts channel settings (internal/server/secrets).
type Sealer interface {
	Encrypt(plaintext, associatedData []byte) ([]byte, error)
	Decrypt(sealed, associatedData []byte) ([]byte, error)
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
	ID        int64
	AgentID   int64
	AgentName string
	RuleID    *int64
	RuleName  string
	Kind      string
	Subject   string
	Severity  string
	Message   string
	State     string
	// StartedAt and ResolvedAt are UTC Unix milliseconds; ResolvedAt is 0
	// while firing.
	StartedAt  int64
	ResolvedAt int64
	// AcknowledgedBy is who acknowledged a firing alert, or "".
	AcknowledgedBy string
	AcknowledgedAt int64
}

// Options configures a Service.
type Options struct {
	Database Database
	AuditLog AuditLog
	Sealer   Sealer
	Clock    Clock
	Logger   *slog.Logger
	// HTTPClient sends webhook, ntfy and Discord notifications; nil uses one
	// with a time limit.
	HTTPClient *http.Client
	// Online says whether a machine is connected; offline rules need it.
	Online func(agentID int64) bool
	// Changed hears when the rules evaluated here (metric, offline and
	// failed-script rules) start or resolve an alert on a machine, so
	// browsers can be told; storage alerts are announced by the hub.
	Changed func(agentID int64, publicID string)
}

// Service keeps alerts, evaluates rules and sends notifications.
type Service struct {
	database   Database
	audit      AuditLog
	sealer     Sealer
	clock      Clock
	logger     *slog.Logger
	httpClient *http.Client
	online     func(agentID int64) bool
	changed    func(agentID int64, publicID string)
	// sending tracks notifications on their way, for Wait.
	sending sync.WaitGroup
}

// NewService returns a Service.
func NewService(options Options) *Service {
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}
	online := options.Online
	if online == nil {
		online = func(int64) bool { return false }
	}
	changed := options.Changed
	if changed == nil {
		changed = func(int64, string) {}
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{
		database: options.Database, audit: options.AuditLog, sealer: options.Sealer, clock: options.Clock,
		logger: logger, httpClient: client, online: online, changed: changed,
	}
}

// Wait waits for notifications on their way; tests and shutdown call it.
func (s *Service) Wait() {
	s.sending.Wait()
}

// scope is the set of alerts a reconcile covers: a machine's firing
// alerts of these kinds, and of only this subject if one is given.
type scope struct {
	agentID int64
	kinds   []string
	subject string
	// quiet leaves notifying to the caller: updates go out as a digest.
	quiet bool
}

// change is an alert that started or resolved.
type change struct {
	alert Alert
	event string
}

// reconcile makes a machine's firing alerts in scope match conditions:
// new conditions start firing, ones that hold keep firing (with the
// newest message) and the rest are resolved. It notifies the channels of
// each alert's rule about what started and resolved.
func (s *Service) reconcile(ctx context.Context, in scope, conditions []Condition) (bool, error) {
	now := s.clock.Now().UnixMilli()
	var changes []change
	changed := false
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		firing, err := firingIn(ctx, tx, in)
		if err != nil {
			return err
		}
		var started []change
		if changed, started, err = raise(ctx, tx, in.agentID, firing, conditions, now); err != nil {
			return err
		}
		changes = started
		for _, alert := range firing {
			changed = true
			if _, err := tx.ExecContext(ctx, `UPDATE alerts SET state = 'resolved', resolved_at = ? WHERE id = ?`, now, alert.ID); err != nil {
				return fmt.Errorf("resolve alert %d: %w", alert.ID, err)
			}
			changes = append(changes, change{alert: alert, event: EventResolved})
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("reconcile alerts: %w", err)
	}
	if !in.quiet {
		s.notify(context.WithoutCancel(ctx), in.agentID, changes)
	}
	return changed, nil
}

// raise starts or updates an alert for each condition, removing from
// firing the alerts that still hold, so only those to resolve are left.
func raise(ctx context.Context, tx *sql.Tx, agentID int64, firing map[string]Alert, conditions []Condition, now int64) (bool, []change, error) {
	changed := false
	var started []change
	for _, condition := range conditions {
		key := condition.Kind + "\x00" + condition.Subject
		alert, found := firing[key]
		delete(firing, key)
		if found && alert.Message == condition.Message && alert.Severity == condition.Severity {
			continue
		}
		changed = true
		id, err := upsert(ctx, tx, agentID, alert, found, condition, now)
		if err != nil {
			return false, nil, err
		}
		if !found {
			ruleID := condition.RuleID
			started = append(started, change{event: EventFiring, alert: Alert{
				ID: id, AgentID: agentID, RuleID: &ruleID, Kind: condition.Kind, Subject: condition.Subject,
				Severity: condition.Severity, Message: condition.Message, State: Firing, StartedAt: now,
			}})
		}
	}
	return changed, started, nil
}

func upsert(ctx context.Context, tx *sql.Tx, agentID int64, alert Alert, found bool, condition Condition, now int64) (int64, error) {
	if found {
		_, err := tx.ExecContext(ctx, `UPDATE alerts SET severity = ?, message = ? WHERE id = ?`,
			condition.Severity, condition.Message, alert.ID)
		if err != nil {
			return 0, fmt.Errorf("update alert %d: %w", alert.ID, err)
		}
		return alert.ID, nil
	}
	var ruleID any
	if condition.RuleID != 0 {
		ruleID = condition.RuleID
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO alerts (agent_id, rule_id, kind, subject, severity, message, state, started_at)
		VALUES (?, ?, ?, ?, ?, ?, 'firing', ?)`, agentID, ruleID, condition.Kind, condition.Subject, condition.Severity, condition.Message, now)
	if err != nil {
		return 0, fmt.Errorf("raise alert %s for %s: %w", condition.Kind, condition.Subject, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("raise alert %s for %s: %w", condition.Kind, condition.Subject, err)
	}
	return id, nil
}

// firingIn returns a machine's firing alerts in scope, keyed by kind and
// subject.
func firingIn(ctx context.Context, tx *sql.Tx, in scope) (map[string]Alert, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(in.kinds)), ", ")
	args := []any{in.agentID}
	for _, kind := range in.kinds {
		args = append(args, kind)
	}
	query := alertSelect + ` WHERE a.agent_id = ? AND a.state = 'firing' AND a.kind IN (` + placeholders + `)`
	if in.subject != "" {
		query += ` AND a.subject = ?`
		args = append(args, in.subject)
	}
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

const alertSelect = `SELECT a.id, a.agent_id, CASE WHEN g.display_name = '' THEN g.hostname ELSE g.display_name END, a.rule_id,
	COALESCE(r.name, ''), a.kind, a.subject, a.severity, a.message, a.state, a.started_at, COALESCE(a.resolved_at, 0),
	COALESCE(u.username, ''), COALESCE(a.acknowledged_at, 0)
	FROM alerts a JOIN agents g ON g.id = a.agent_id LEFT JOIN alert_rules r ON r.id = a.rule_id
	LEFT JOIN users u ON u.id = a.acknowledged_by`

func queryAlerts(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Alert, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	alerts := []Alert{}
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.AgentID, &a.AgentName, &a.RuleID, &a.RuleName, &a.Kind, &a.Subject, &a.Severity, &a.Message,
			&a.State, &a.StartedAt, &a.ResolvedAt, &a.AcknowledgedBy, &a.AcknowledgedAt); err != nil {
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
	return s.list(ctx, ` AND a.agent_id = ?`, []any{agentID}, limit)
}

// List returns every firing alert, most severe and newest first, then up
// to limit resolved ones, newest first.
func (s *Service) List(ctx context.Context, limit int) ([]Alert, error) {
	return s.list(ctx, "", nil, limit)
}

func (s *Service) list(ctx context.Context, filter string, args []any, limit int) ([]Alert, error) {
	var alerts []Alert
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		firing, err := queryAlerts(ctx, tx, alertSelect+` WHERE a.state = 'firing'`+filter+`
			ORDER BY a.severity = 'critical' DESC, a.started_at DESC, a.id DESC`, args...)
		if err != nil {
			return err
		}
		resolved, err := queryAlerts(ctx, tx, alertSelect+` WHERE a.state = 'resolved'`+filter+`
			ORDER BY a.resolved_at DESC, a.id DESC LIMIT ?`, append(args, limit)...)
		alerts = slices.Concat(firing, resolved)
		return err
	})
	return alerts, err
}

// Acknowledge marks a firing alert as seen, and audits it. The alert keeps
// firing until its condition clears; acknowledging only says someone is
// on it.
func (s *Service) Acknowledge(ctx context.Context, actor auth.Session, request auth.RequestInfo, id int64) error {
	if err := auth.Authorize(actor, auth.PermissionManageAlerts); err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		var agentID int64
		err := tx.QueryRowContext(ctx, `UPDATE alerts SET acknowledged_by = ?, acknowledged_at = ? WHERE id = ? AND state = 'firing'
			RETURNING agent_id`, actor.UserID, s.clock.Now().UnixMilli(), id).Scan(&agentID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("acknowledge the alert: %w", err)
		}
		entry := userEntry(actor, request, "alert.acknowledge", map[string]any{"alert": id})
		entry.AgentID = &agentID
		return s.audit.RecordInTx(ctx, tx, entry)
	})
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

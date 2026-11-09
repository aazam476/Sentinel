// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// machine is what rules and notifications need to know about a machine.
type machine struct {
	id               int64
	publicID         string
	name             string
	tags             []string
	lastSeenAt       *int64
	maintenanceUntil *int64
}

func (m machine) inMaintenance(now int64) bool {
	return m.maintenanceUntil != nil && *m.maintenanceUntil > now
}

const machineSelect = `SELECT g.id, g.public_id, CASE WHEN g.display_name = '' THEN g.hostname ELSE g.display_name END,
	g.last_seen_at, g.maintenance_until, COALESCE((SELECT group_concat(tag, char(31)) FROM agent_tags t WHERE t.agent_id = g.id), '')
	FROM agents g WHERE g.revoked_at IS NULL`

func readMachines(ctx context.Context, tx *sql.Tx, filter string, args ...any) ([]machine, error) {
	rows, err := tx.QueryContext(ctx, machineSelect+filter, args...)
	if err != nil {
		return nil, fmt.Errorf("list machines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var machines []machine
	for rows.Next() {
		var m machine
		var tags string
		if err := rows.Scan(&m.id, &m.publicID, &m.name, &m.lastSeenAt, &m.maintenanceUntil, &tags); err != nil {
			return nil, fmt.Errorf("read a machine: %w", err)
		}
		if tags != "" {
			m.tags = strings.Split(tags, "\x1f")
		}
		machines = append(machines, m)
	}
	return machines, rows.Err()
}

func readMachine(ctx context.Context, tx *sql.Tx, agentID int64) (machine, bool, error) {
	machines, err := readMachines(ctx, tx, ` AND g.id = ?`, agentID)
	if err != nil || len(machines) == 0 {
		return machine{}, false, err
	}
	return machines[0], true, nil
}

// notify sends what started and resolved to the channels of each alert's
// rule, in the background, unless the machine is in maintenance.
func (s *Service) notify(ctx context.Context, agentID int64, changes []change) {
	if len(changes) == 0 {
		return
	}
	var target machine
	var found bool
	deliveries := map[int64][]Channel{}
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		if target, found, err = readMachine(ctx, tx, agentID); err != nil || !found {
			return err
		}
		for _, c := range changes {
			if c.alert.RuleID == nil {
				continue
			}
			if _, done := deliveries[*c.alert.RuleID]; done {
				continue
			}
			channels, err := s.readChannels(ctx, tx, `SELECT `+channelColumns+` FROM notification_channels n
				JOIN alert_rule_channels rc ON rc.channel_id = n.id WHERE rc.rule_id = ? AND n.enabled = 1`, *c.alert.RuleID)
			if err != nil {
				return err
			}
			deliveries[*c.alert.RuleID] = channels
		}
		return nil
	})
	if err != nil {
		s.logger.Error("could not look up who to notify", "agent", agentID, "error", err)
		return
	}
	if !found || target.inMaintenance(s.clock.Now().UnixMilli()) {
		return
	}
	s.send(ctx, target, changes, deliveries)
}

func (s *Service) send(ctx context.Context, target machine, changes []change, deliveries map[int64][]Channel) {
	ruleNames := s.ruleNames(ctx)
	for _, c := range changes {
		if c.alert.RuleID == nil {
			continue
		}
		n := Notification{
			Event: c.event, AlertID: c.alert.ID, Machine: target.name, Rule: ruleNames[*c.alert.RuleID],
			Severity: c.alert.Severity, Message: c.alert.Message, At: s.clock.Now(),
		}
		for _, channel := range deliveries[*c.alert.RuleID] {
			s.sending.Go(func() { _ = s.deliver(ctx, channel, n) }) // failures are recorded on the channel
		}
	}
}

func (s *Service) ruleNames(ctx context.Context) map[int64]string {
	names := map[int64]string{}
	_ = s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, name FROM alert_rules`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			var name string
			if rows.Scan(&id, &name) == nil {
				names[id] = name
			}
		}
		return rows.Err()
	}) // a missing name only leaves the notification's rule blank
	return names
}

// minutesAgo is the time some minutes before now, in milliseconds.
func (s *Service) minutesAgo(minutes int) int64 {
	return s.clock.Now().Add(-time.Duration(minutes) * time.Minute).UnixMilli()
}

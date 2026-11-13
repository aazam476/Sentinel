// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

// KindUpdatesAvailable is the alert a machine with waiting operating
// system updates has (§9.9). Its channels hear about it once a day, in a
// digest of every machine, rather than once per machine.
const KindUpdatesAvailable = "updates.available"

// packageCheckTimeout bounds asking one machine for its updates: it
// refreshes every package catalog first.
const packageCheckTimeout = 10 * time.Minute

// packageChecksAtOnce is how many machines are asked at the same time.
const packageChecksAtOnce = 8

// Machines asks connected agents for things (internal/server/hub).
type Machines interface {
	Online() []int64
	RequestWithin(ctx context.Context, agentID int64, timeout time.Duration, action string, payload, result any) error
}

// maxDigestMachines is how many machines a digest names; the rest are
// counted.
const maxDigestMachines = 20

// UpdatesCondition is the alert a machine's waiting updates call for, or
// nil when it has none.
func UpdatesCondition(list protocol.PackageUpdatesPayload) *Condition {
	total, security := 0, 0
	for _, source := range list.Sources {
		total += len(source.Updates)
		for _, update := range source.Updates {
			if update.Security {
				security++
			}
		}
	}
	if total == 0 {
		return nil
	}
	message := plural(total, "update") + " available"
	if security > 0 {
		message += fmt.Sprintf(", %d of them security fixes", security)
	}
	return &Condition{Kind: KindUpdatesAvailable, Severity: Warning, Message: message + "."}
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// RecordPackageUpdates raises or resolves a machine's updates alert, by
// the first enabled updates rule that watches it, without notifying:
// SendUpdatesDigest does that. It returns whether the alert changed.
func (s *Service) RecordPackageUpdates(ctx context.Context, agentID int64, list protocol.PackageUpdatesPayload) (bool, error) {
	var rules []Rule
	var target machine
	var found bool
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		if rules, err = readRules(ctx, tx, RuleUpdates); err != nil {
			return err
		}
		target, found, err = readMachine(ctx, tx, agentID)
		return err
	})
	if err != nil || !found {
		return false, err
	}
	var conditions []Condition
	if condition := UpdatesCondition(list); condition != nil {
		for _, rule := range rules {
			if rule.appliesTo(target) {
				condition.RuleID = rule.ID
				conditions = append(conditions, *condition)
				break
			}
		}
	}
	changed, err := s.reconcile(ctx, scope{agentID: agentID, kinds: []string{KindUpdatesAvailable}, quiet: true}, conditions)
	if changed {
		s.changed(target.id, target.publicID)
	}
	return changed, err
}

// CheckPackages asks every online machine for its waiting operating
// system updates, raises or resolves each one's alert, then sends the
// day's digest (§9.9). A machine that can't be asked (no package manager,
// switched off by its policy, offline by now) is skipped and keeps its
// alert as it was.
func (s *Service) CheckPackages(ctx context.Context, machines Machines) error {
	limit := make(chan struct{}, packageChecksAtOnce)
	var checks sync.WaitGroup
	for _, agentID := range machines.Online() {
		limit <- struct{}{}
		checks.Go(func() {
			defer func() { <-limit }()
			var list protocol.PackageUpdatesPayload
			if err := machines.RequestWithin(ctx, agentID, packageCheckTimeout, protocol.ActionPackageListUpdates, nil, &list); err != nil {
				s.logger.Debug("skipped a machine's package update check", "agent", agentID, "error", err)
				return
			}
			if _, err := s.RecordPackageUpdates(ctx, agentID, list); err != nil {
				s.logger.Warn("could not record a machine's updates", "agent", agentID, "error", err)
			}
		})
	}
	checks.Wait()
	return s.SendUpdatesDigest(ctx)
}

// digestLine is one machine in a digest.
type digestLine struct {
	ruleID  int64
	machine string
	message string
}

// SendUpdatesDigest tells each updates rule's channels, in one
// notification, which machines have updates waiting. Machines in
// maintenance are left out, and a rule with none sends nothing.
func (s *Service) SendUpdatesDigest(ctx context.Context) error {
	lines, err := s.readDigest(ctx)
	if err != nil {
		return err
	}
	byRule := map[int64][]digestLine{}
	for _, line := range lines {
		byRule[line.ruleID] = append(byRule[line.ruleID], line)
	}
	ruleNames := s.ruleNames(ctx)
	for ruleID, machines := range byRule {
		var channels []Channel
		err := s.database.Read(ctx, func(tx *sql.Tx) error {
			var err error
			channels, err = s.readChannels(ctx, tx, `SELECT `+channelColumns+` FROM notification_channels n
				JOIN alert_rule_channels rc ON rc.channel_id = n.id WHERE rc.rule_id = ? AND n.enabled = 1`, ruleID)
			return err
		})
		if err != nil {
			return fmt.Errorf("read the updates rule's channels: %w", err)
		}
		n := digest(machines)
		n.Rule, n.At = ruleNames[ruleID], s.clock.Now()
		for _, channel := range channels {
			s.sending.Go(func() { _ = s.deliver(context.WithoutCancel(ctx), channel, n) }) // failures are recorded on the channel
		}
	}
	return nil
}

// readDigest lists the machines with a firing updates alert, by name.
func (s *Service) readDigest(ctx context.Context) ([]digestLine, error) {
	now := s.clock.Now().UnixMilli()
	var lines []digestLine
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT a.rule_id, CASE WHEN g.display_name = '' THEN g.hostname ELSE g.display_name END, a.message
			FROM alerts a JOIN agents g ON g.id = a.agent_id
			WHERE a.kind = ? AND a.state = 'firing' AND a.rule_id IS NOT NULL AND g.revoked_at IS NULL
			AND (g.maintenance_until IS NULL OR g.maintenance_until <= ?)`, KindUpdatesAvailable, now)
		if err != nil {
			return fmt.Errorf("list machines with updates: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var line digestLine
			if err := rows.Scan(&line.ruleID, &line.machine, &line.message); err != nil {
				return fmt.Errorf("read a machine with updates: %w", err)
			}
			lines = append(lines, line)
		}
		return rows.Err()
	})
	slices.SortFunc(lines, func(a, b digestLine) int { return strings.Compare(a.machine, b.machine) })
	return lines, err
}

// digest is the notification for one rule's machines.
func digest(machines []digestLine) Notification {
	n := Notification{Event: EventFiring, Severity: Warning}
	if len(machines) == 1 {
		n.Machine, n.Message = machines[0].machine, machines[0].message
		return n
	}
	n.Machine = plural(len(machines), "machine")
	parts := make([]string, 0, min(len(machines), maxDigestMachines)+1)
	for _, line := range machines[:min(len(machines), maxDigestMachines)] {
		parts = append(parts, line.machine+": "+strings.TrimSuffix(line.message, "."))
	}
	if extra := len(machines) - maxDigestMachines; extra > 0 {
		parts = append(parts, "and "+plural(extra, "more machine"))
	}
	n.Message = "Updates are waiting on " + n.Machine + ". " + strings.Join(parts, "; ") + "."
	return n
}

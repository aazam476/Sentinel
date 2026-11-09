// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
)

// metricNames are how a metric rule's alert names its metric.
func metricNames() map[string]string {
	return map[string]string{
		"cpu.percent": "CPU", "memory.percent": "Memory", "disk.percent": "Disk", "temperature": "Temperature",
	}
}

// metricUnit is what follows a metric's value in a message.
func metricUnit(metric string) string {
	if metric == "temperature" {
		return " °C"
	}
	return "%"
}

// Evaluate checks the metric and offline rules against every machine, and
// raises or resolves their alerts. The scheduler calls it every minute.
func (s *Service) Evaluate(ctx context.Context) error {
	var rules []Rule
	var machines []machine
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		if rules, err = readRules(ctx, tx, ""); err != nil {
			return err
		}
		machines, err = readMachines(ctx, tx, "")
		return err
	})
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.Type != RuleMetric && rule.Type != RuleOffline {
			continue
		}
		for _, target := range machines {
			if err := s.evaluateRule(ctx, rule, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// evaluateRule raises or resolves one rule's alert on one machine. A
// metric rule with no readings in its window (the machine is off) leaves
// its alert as it is: the offline rule covers that.
func (s *Service) evaluateRule(ctx context.Context, rule Rule, target machine) error {
	in := scope{agentID: target.id, kinds: []string{rule.kind()}}
	if !rule.Enabled || !rule.appliesTo(target) {
		return s.reconcileAndAnnounce(ctx, target, in, nil)
	}
	var condition *Condition
	var known bool
	var err error
	if rule.Type == RuleOffline {
		condition, known = s.offlineCondition(rule, target), true
	} else {
		condition, known, err = s.metricCondition(ctx, rule, target)
	}
	if err != nil || !known {
		return err
	}
	var conditions []Condition
	if condition != nil {
		conditions = []Condition{*condition}
	}
	return s.reconcileAndAnnounce(ctx, target, in, conditions)
}

// reconcileAndAnnounce reconciles, and says so if anything changed.
func (s *Service) reconcileAndAnnounce(ctx context.Context, target machine, in scope, conditions []Condition) error {
	changed, err := s.reconcile(ctx, in, conditions)
	if changed {
		s.changed(target.id, target.publicID)
	}
	return err
}

func (s *Service) offlineCondition(rule Rule, target machine) *Condition {
	if s.online(target.id) {
		return nil
	}
	if target.lastSeenAt != nil && *target.lastSeenAt > s.minutesAgo(rule.Config.Minutes) {
		return nil
	}
	return &Condition{
		RuleID: rule.ID, Kind: rule.kind(), Severity: Critical,
		Message: fmt.Sprintf("Offline for more than %s.", minutes(rule.Config.Minutes)),
	}
}

// metricCondition reads the rule's metric over its window. The alert
// fires when every reading is past the threshold and the readings cover
// the whole window (allowing for one missed sample at its start).
func (s *Service) metricCondition(ctx context.Context, rule Rule, target machine) (*Condition, bool, error) {
	config := rule.Config
	from := s.minutesAgo(config.Minutes)
	var count int
	var lowest, highest sql.NullFloat64
	var earliest sql.NullInt64
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*), MIN(value), MAX(value), MIN(ts) FROM metrics_raw
			WHERE agent_id = ? AND metric = ? AND ts >= ?`, target.id, config.Metric, from).Scan(&count, &lowest, &highest, &earliest)
	})
	if err != nil {
		return nil, false, fmt.Errorf("read %s for a rule: %w", config.Metric, err)
	}
	if count == 0 {
		return nil, false, nil
	}
	const slack = 30_000 // three samples' worth
	covered := earliest.Int64 <= from+slack
	past := (config.Direction == "above" && lowest.Float64 > config.Threshold) ||
		(config.Direction == "below" && highest.Float64 < config.Threshold)
	if !covered || !past {
		return nil, true, nil
	}
	value := highest.Float64
	if config.Direction == "below" {
		value = lowest.Float64
	}
	return &Condition{
		RuleID: rule.ID, Kind: rule.kind(), Severity: Warning,
		Message: fmt.Sprintf("%s has been %s %s%s for %s (%s%s).", metricNames()[config.Metric], config.Direction,
			formatValue(config.Threshold), metricUnit(config.Metric), minutes(config.Minutes), formatValue(value), metricUnit(config.Metric)),
	}, true, nil
}

// minutes is a count of minutes in words: "1 minute", "5 minutes".
func minutes(count int) string {
	if count == 1 {
		return "1 minute"
	}
	return strconv.Itoa(count) + " minutes"
}

func formatValue(value float64) string {
	if value == math.Trunc(value) {
		return strconv.FormatFloat(value, 'f', 0, 64)
	}
	return strconv.FormatFloat(value, 'f', 1, 64)
}

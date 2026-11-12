// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Rule types.
const (
	RuleMetric       = "metric"
	RuleOffline      = "offline"
	RuleStorage      = "storage"
	RuleScriptFailed = "script_failed"
	RuleContainer    = "container"
)

// ErrNotFound means no rule, channel or alert has that ID.
var ErrNotFound = errors.New("not found")

// InvalidError says which field of a rule or channel is wrong, and why.
type InvalidError struct {
	Field, Reason string
}

func (e *InvalidError) Error() string { return e.Field + ": " + e.Reason }

// Metrics a metric rule can watch (internal/server/metrics names).
func watchableMetrics() []string {
	return []string{"cpu.percent", "memory.percent", "disk.percent", "temperature"}
}

// RuleConfig holds the settings each rule type uses.
type RuleConfig struct {
	// Metric rules: the metric, "above" or "below", the threshold, and how
	// many minutes every reading must be past it.
	Metric    string  `json:"metric,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
	// Minutes is also how long a machine must be offline for an offline rule.
	Minutes int `json:"minutes,omitempty"`
	// CapacityPercent is how full a pool or quota gets before a storage
	// rule alerts.
	CapacityPercent uint64 `json:"capacityPercent,omitempty"`
}

// RuleTarget says which machines a rule watches.
type RuleTarget struct {
	// Kind is "all", "tag" (Value is a tag) or "agent" (a machine's ID).
	Kind  string
	Value string
}

// Rule is one alert rule.
type Rule struct {
	ID         int64
	Name       string
	Type       string
	Config     RuleConfig
	Target     RuleTarget
	Enabled    bool
	ChannelIDs []int64
}

// validate checks a rule and fills in its defaults.
func (r *Rule) validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || utf8.RuneCountInString(r.Name) > 100 {
		return &InvalidError{"name", "give a name of up to 100 characters"}
	}
	if err := r.validateConfig(); err != nil {
		return err
	}
	switch r.Target.Kind {
	case "all":
		r.Target.Value = ""
	case "tag", "agent":
		if strings.TrimSpace(r.Target.Value) == "" {
			return &InvalidError{"target", "choose the tag or machine"}
		}
	default:
		return &InvalidError{"target", "choose all machines, a tag or one machine"}
	}
	return nil
}

func (r *Rule) validateConfig() error {
	c := &r.Config
	switch r.Type {
	case RuleMetric:
		if !slices.Contains(watchableMetrics(), c.Metric) {
			return &InvalidError{"metric", "choose cpu.percent, memory.percent, disk.percent or temperature"}
		}
		if c.Direction != "above" && c.Direction != "below" {
			return &InvalidError{"direction", "choose above or below"}
		}
		if c.Threshold < 0 || c.Threshold > 1000 {
			return &InvalidError{"threshold", "choose a threshold from 0 to 1000"}
		}
		if c.Minutes < 1 || c.Minutes > 1440 {
			return &InvalidError{"minutes", "choose from 1 minute to a day"}
		}
		*c = RuleConfig{Metric: c.Metric, Direction: c.Direction, Threshold: c.Threshold, Minutes: c.Minutes}
	case RuleOffline:
		if c.Minutes < 1 || c.Minutes > 10080 {
			return &InvalidError{"minutes", "choose from 1 minute to a week"}
		}
		*c = RuleConfig{Minutes: c.Minutes}
	case RuleStorage:
		if c.CapacityPercent == 0 {
			c.CapacityPercent = DefaultCapacityPercent
		}
		if c.CapacityPercent > 100 {
			return &InvalidError{"capacityPercent", "choose a percentage up to 100"}
		}
		*c = RuleConfig{CapacityPercent: c.CapacityPercent}
	case RuleScriptFailed, RuleContainer:
		*c = RuleConfig{}
	default:
		return &InvalidError{"type", "choose metric, offline, storage, script_failed or container"}
	}
	return nil
}

// appliesTo says whether the rule watches a machine.
func (r Rule) appliesTo(machine machine) bool {
	switch r.Target.Kind {
	case "tag":
		return slices.ContainsFunc(machine.tags, func(tag string) bool { return strings.EqualFold(tag, r.Target.Value) })
	case "agent":
		return machine.publicID == r.Target.Value
	}
	return true
}

// kind is the alert kind a metric, offline or script rule's alerts have.
func (r Rule) kind() string { return "rule." + strconv.FormatInt(r.ID, 10) }

// Rules lists every rule, by name.
func (s *Service) Rules(ctx context.Context) ([]Rule, error) {
	var rules []Rule
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		rules, err = readRules(ctx, tx, "")
		return err
	})
	return rules, err
}

// readRules reads the rules, enabled ones only if onlyType is set, of
// that type.
func readRules(ctx context.Context, tx *sql.Tx, onlyType string) ([]Rule, error) {
	query := `SELECT id, name, type, config_json, target_kind, target_value, enabled FROM alert_rules`
	args := []any{}
	if onlyType != "" {
		query += ` WHERE enabled = 1 AND type = ?`
		args = append(args, onlyType)
	}
	rows, err := tx.QueryContext(ctx, query+` ORDER BY name COLLATE NOCASE, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	rules := []Rule{}
	for rows.Next() {
		var r Rule
		var config string
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &config, &r.Target.Kind, &r.Target.Value, &r.Enabled); err != nil {
			return nil, fmt.Errorf("read an alert rule: %w", err)
		}
		if err := json.Unmarshal([]byte(config), &r.Config); err != nil {
			return nil, fmt.Errorf("read alert rule %d's settings: %w", r.ID, err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	return rules, addChannelIDs(ctx, tx, rules)
}

func addChannelIDs(ctx context.Context, tx *sql.Tx, rules []Rule) error {
	for i := range rules {
		rows, err := tx.QueryContext(ctx, `SELECT channel_id FROM alert_rule_channels WHERE rule_id = ? ORDER BY channel_id`, rules[i].ID)
		if err != nil {
			return fmt.Errorf("read a rule's channels: %w", err)
		}
		rules[i].ChannelIDs = []int64{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return fmt.Errorf("read a rule's channels: %w", err)
			}
			rules[i].ChannelIDs = append(rules[i].ChannelIDs, id)
		}
		_ = rows.Close()
	}
	return nil
}

// SaveRule adds a rule (ID 0) or changes one, and audits it.
func (s *Service) SaveRule(ctx context.Context, actor auth.Session, request auth.RequestInfo, rule Rule) (Rule, error) {
	if err := auth.Authorize(actor, auth.PermissionManageAlerts); err != nil {
		return Rule{}, err
	}
	if err := rule.validate(); err != nil {
		return Rule{}, err
	}
	config, err := json.Marshal(rule.Config)
	if err != nil {
		return Rule{}, fmt.Errorf("save the rule: %w", err)
	}
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		if err := writeRule(ctx, tx, &rule, string(config), s.clock.Now().UnixMilli()); err != nil {
			return err
		}
		if err := setRuleChannels(ctx, tx, rule.ID, rule.ChannelIDs); err != nil {
			return err
		}
		return s.audit.RecordInTx(ctx, tx, userEntry(actor, request, "alert.rule.save", map[string]any{
			"rule": rule.ID, "name": rule.Name, "type": rule.Type, "enabled": rule.Enabled,
		}))
	})
	return rule, err
}

func writeRule(ctx context.Context, tx *sql.Tx, rule *Rule, config string, now int64) error {
	if rule.ID == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO alert_rules (name, type, config_json, target_kind, target_value, enabled, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, rule.Name, rule.Type, config, rule.Target.Kind, rule.Target.Value, rule.Enabled, now)
		if err == nil {
			rule.ID, err = result.LastInsertId()
		}
		if err != nil {
			return fmt.Errorf("add the rule: %w", err)
		}
		return nil
	}
	result, err := tx.ExecContext(ctx, `UPDATE alert_rules SET name = ?, type = ?, config_json = ?, target_kind = ?, target_value = ?,
		enabled = ? WHERE id = ?`, rule.Name, rule.Type, config, rule.Target.Kind, rule.Target.Value, rule.Enabled, rule.ID)
	if err != nil {
		return fmt.Errorf("change the rule: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	return nil
}

func setRuleChannels(ctx context.Context, tx *sql.Tx, ruleID int64, channelIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_rule_channels WHERE rule_id = ?`, ruleID); err != nil {
		return fmt.Errorf("set the rule's channels: %w", err)
	}
	for _, channelID := range channelIDs {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO alert_rule_channels (rule_id, channel_id)
			SELECT ?, id FROM notification_channels WHERE id = ?`, ruleID, channelID)
		if err != nil {
			return fmt.Errorf("set the rule's channels: %w", err)
		}
	}
	return nil
}

// DeleteRule removes a rule and resolves the alerts it raised.
func (s *Service) DeleteRule(ctx context.Context, actor auth.Session, request auth.RequestInfo, id int64) error {
	if err := auth.Authorize(actor, auth.PermissionManageAlerts); err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE alerts SET state = 'resolved', resolved_at = ? WHERE rule_id = ? AND state = 'firing'`,
			s.clock.Now().UnixMilli(), id); err != nil {
			return fmt.Errorf("resolve the rule's alerts: %w", err)
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete the rule: %w", err)
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return ErrNotFound
		}
		return s.audit.RecordInTx(ctx, tx, userEntry(actor, request, "alert.rule.delete", map[string]any{"rule": id}))
	})
}

func userEntry(actor auth.Session, request auth.RequestInfo, action string, details map[string]any) audit.Entry {
	userID := actor.UserID
	return audit.Entry{UserID: &userID, Username: actor.Username, Action: action, Outcome: audit.OutcomeSuccess, IP: request.IP, Details: details}
}

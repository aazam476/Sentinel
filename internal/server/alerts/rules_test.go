// SPDX-License-Identifier: Apache-2.0

package alerts_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/auth"
)

func cpuRule(direction string, threshold float64) alerts.Rule {
	return alerts.Rule{
		Name: "CPU busy", Type: alerts.RuleMetric, Enabled: true, Target: alerts.RuleTarget{Kind: "all"},
		Config: alerts.RuleConfig{Metric: "cpu.percent", Direction: direction, Threshold: threshold, Minutes: 5},
	}
}

func TestSaveRuleValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(rule *alerts.Rule)
		field  string
	}{
		{"refuses a rule with no name", func(r *alerts.Rule) { r.Name = "  " }, "name"},
		{"refuses a name over 100 characters", func(r *alerts.Rule) { r.Name = strings.Repeat("n", 101) }, "name"},
		{"refuses an unknown type", func(r *alerts.Rule) { r.Type = "weather" }, "type"},
		{"refuses a metric it can't watch", func(r *alerts.Rule) { r.Config.Metric = "network.rx" }, "metric"},
		{"refuses a direction other than above or below", func(r *alerts.Rule) { r.Config.Direction = "around" }, "direction"},
		{"refuses a negative threshold", func(r *alerts.Rule) { r.Config.Threshold = -1 }, "threshold"},
		{"refuses a window of no minutes", func(r *alerts.Rule) { r.Config.Minutes = 0 }, "minutes"},
		{"refuses a window over a day", func(r *alerts.Rule) { r.Config.Minutes = 1441 }, "minutes"},
		{"refuses an offline rule of no minutes", func(r *alerts.Rule) { r.Type, r.Config = alerts.RuleOffline, alerts.RuleConfig{} }, "minutes"},
		{"refuses a capacity over 100%", func(r *alerts.Rule) {
			r.Type, r.Config = alerts.RuleStorage, alerts.RuleConfig{CapacityPercent: 101}
		}, "capacityPercent"},
		{"refuses a tag target with no tag", func(r *alerts.Rule) { r.Target = alerts.RuleTarget{Kind: "tag"} }, "target"},
		{"refuses an unknown target", func(r *alerts.Rule) { r.Target = alerts.RuleTarget{Kind: "everyone"} }, "target"},
	}
	h := newHarness(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := cpuRule("above", 90)
			tc.change(&rule)
			_, err := h.service.SaveRule(t.Context(), h.admin, auth.RequestInfo{}, rule)
			var invalid *alerts.InvalidError
			if !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Errorf("got %v, want a problem with %s", err, tc.field)
			}
		})
	}
}

func TestRules(t *testing.T) {
	t.Run("starts with the storage health rule M5's alerts came from", func(t *testing.T) {
		h := newHarness(t)
		rules, err := h.service.Rules(t.Context())
		if err != nil || len(rules) != 1 || rules[0].Type != alerts.RuleStorage || rules[0].Config.CapacityPercent != 80 || !rules[0].Enabled {
			t.Errorf("got %+v, %v", rules, err)
		}
	})

	t.Run("adds, changes and deletes a rule, keeping only the settings its type uses, and audits each", func(t *testing.T) {
		h := newHarness(t)
		channel := saveWebhook(t, h)
		rule := cpuRule("above", 90)
		rule.Config.CapacityPercent = 50 // not a metric rule's setting
		rule.ChannelIDs = []int64{channel.ID, 9999}
		saved, err := h.service.SaveRule(t.Context(), h.admin, auth.RequestInfo{IP: "192.0.2.1"}, rule)
		if err != nil || saved.ID == 0 || saved.Config.CapacityPercent != 0 {
			t.Fatalf("got %+v, %v", saved, err)
		}
		saved.Name, saved.Config.Threshold = "CPU very busy", 95
		if _, err := h.service.SaveRule(t.Context(), h.admin, auth.RequestInfo{}, saved); err != nil {
			t.Fatal(err)
		}
		rules, err := h.service.Rules(t.Context())
		if err != nil || len(rules) != 2 || rules[0].Name != "CPU very busy" || rules[0].Config.Threshold != 95 ||
			len(rules[0].ChannelIDs) != 1 || rules[0].ChannelIDs[0] != channel.ID {
			t.Errorf("got %+v, %v", rules, err)
		}
		if err := h.service.DeleteRule(t.Context(), h.admin, auth.RequestInfo{}, saved.ID); err != nil {
			t.Fatal(err)
		}
		if err := h.service.DeleteRule(t.Context(), h.admin, auth.RequestInfo{}, saved.ID); !errors.Is(err, alerts.ErrNotFound) {
			t.Errorf("deleting it again: %v", err)
		}
		saved.ID = 9999
		if _, err := h.service.SaveRule(t.Context(), h.admin, auth.RequestInfo{}, saved); !errors.Is(err, alerts.ErrNotFound) {
			t.Errorf("changing a missing rule: %v", err)
		}
		if got := auditActions(t, h); !equalStrings(got, []string{"alert.channel.save", "alert.rule.save", "alert.rule.save", "alert.rule.delete"}) {
			t.Errorf("audited %v", got)
		}
	})

	t.Run("refuses anyone who can't manage alerts", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.SaveRule(t.Context(), h.viewer, auth.RequestInfo{}, cpuRule("above", 90)); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("save: %v", err)
		}
		if err := h.service.DeleteRule(t.Context(), h.viewer, auth.RequestInfo{}, 1); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("delete: %v", err)
		}
		if err := h.service.Acknowledge(t.Context(), h.viewer, auth.RequestInfo{}, 1); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("acknowledge: %v", err)
		}
	})
}

func auditActions(t *testing.T, h *harness) []string {
	t.Helper()
	var actions []string
	err := h.database.Read(t.Context(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT action FROM audit_log ORDER BY id`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var action string
			if err := rows.Scan(&action); err != nil {
				return err
			}
			actions = append(actions, action)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return actions
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, "\n") == strings.Join(b, "\n")
}

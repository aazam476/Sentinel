// SPDX-License-Identifier: Apache-2.0

package alerts_test

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// addReadings stores a reading every 10 seconds for the last minutes.
func (h *harness) addReadings(t *testing.T, agentID int64, metric string, value float64, minutes int) {
	t.Helper()
	now := h.clock.Now().UnixMilli()
	for at := now - int64(minutes)*60_000; at <= now; at += 10_000 {
		h.exec(t, `INSERT OR REPLACE INTO metrics_raw (agent_id, metric, ts, value) VALUES (?, ?, ?, ?)`, agentID, metric, at, value)
	}
}

func (h *harness) saveRule(t *testing.T, rule alerts.Rule) alerts.Rule {
	t.Helper()
	saved, err := h.service.SaveRule(t.Context(), h.admin, auth.RequestInfo{}, rule)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func (h *harness) evaluate(t *testing.T) []string {
	t.Helper()
	if err := h.service.Evaluate(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.service.Wait()
	return states(t, h)
}

// events lists the notifications the receiver got, as "event: message".
func (h *harness) events(t *testing.T) []string {
	t.Helper()
	result := []string{}
	for _, got := range h.receiver.take() {
		body := got.JSON(t)
		result = append(result, body["event"].(string)+": "+body["message"].(string))
	}
	slices.Sort(result)
	return result
}

func TestMetricRules(t *testing.T) {
	t.Run("fires once every reading in the window is past the threshold, notifies, and resolves when one isn't", func(t *testing.T) {
		h := newHarness(t)
		rule := cpuRule("above", 90)
		rule.ChannelIDs = []int64{saveWebhook(t, h).ID}
		rule = h.saveRule(t, rule)
		h.addReadings(t, h.agentID, "cpu.percent", 95, 5)
		if got := h.evaluate(t); !slices.Equal(got, []string{"firing rule." + strconv.FormatInt(rule.ID, 10) + " "}) {
			t.Fatalf("got %v", got)
		}
		if got := h.events(t); !slices.Equal(got, []string{"firing: CPU has been above 90% for 5 minutes (95%)."}) {
			t.Errorf("notified %v", got)
		}
		if !slices.Equal(h.changed, []string{nasID}) {
			t.Errorf("announced %v", h.changed)
		}
		h.clock.Advance(time.Minute)
		if got := h.evaluate(t); len(got) != 1 || !strings.HasPrefix(got[0], "firing") {
			t.Errorf("the alert didn't keep firing: %v", got)
		}
		if got := h.events(t); len(got) != 0 {
			t.Errorf("notified again: %v", got)
		}
		h.addReadings(t, h.agentID, "cpu.percent", 40, 0)
		if got := h.evaluate(t); len(got) != 1 || !strings.HasPrefix(got[0], "resolved") {
			t.Errorf("got %v", got)
		}
		if got := h.events(t); !slices.Equal(got, []string{"resolved: CPU has been above 90% for 5 minutes (95%)."}) {
			t.Errorf("notified %v", got)
		}
	})

	t.Run("waits until the readings cover the whole window", func(t *testing.T) {
		h := newHarness(t)
		h.saveRule(t, cpuRule("above", 90))
		h.addReadings(t, h.agentID, "cpu.percent", 99, 2)
		if got := h.evaluate(t); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("fires below a threshold, naming the lowest reading and the unit", func(t *testing.T) {
		h := newHarness(t)
		rule := cpuRule("below", 10.5)
		rule.Config.Metric = "temperature"
		rule.ChannelIDs = []int64{saveWebhook(t, h).ID}
		h.saveRule(t, rule)
		h.addReadings(t, h.agentID, "temperature", 4.25, 5)
		h.evaluate(t)
		if got := h.events(t); !slices.Equal(got, []string{"firing: Temperature has been below 10.5 °C for 5 minutes (4.2 °C)."}) {
			t.Errorf("notified %v", got)
		}
	})

	t.Run("leaves its alert alone while the machine sends nothing", func(t *testing.T) {
		h := newHarness(t)
		h.saveRule(t, cpuRule("above", 90))
		h.addReadings(t, h.agentID, "cpu.percent", 95, 5)
		h.evaluate(t)
		h.clock.Advance(time.Hour)
		if got := h.evaluate(t); len(got) != 1 || !strings.HasPrefix(got[0], "firing") {
			t.Errorf("got %v", got)
		}
	})

	t.Run("resolves its alerts when the rule is turned off or no longer covers the machine", func(t *testing.T) {
		h := newHarness(t)
		rule := h.saveRule(t, cpuRule("above", 90))
		h.addReadings(t, h.agentID, "cpu.percent", 95, 5)
		h.evaluate(t)
		rule.Target = alerts.RuleTarget{Kind: "tag", Value: "web"}
		h.saveRule(t, rule)
		if got := h.evaluate(t); len(got) != 1 || !strings.HasPrefix(got[0], "resolved") {
			t.Errorf("got %v", got)
		}
	})

	t.Run("watches only the machines with its tag, or the one machine it names", func(t *testing.T) {
		h := newHarness(t)
		other := h.addMachine(t, "01JBX3K5Q7Z8M4N6P8R0T2V4W1", "web", "web")
		byTag := cpuRule("above", 90)
		byTag.Target = alerts.RuleTarget{Kind: "tag", Value: "STORAGE"}
		h.saveRule(t, byTag)
		byMachine := cpuRule("above", 50)
		byMachine.Name, byMachine.Target = "web busy", alerts.RuleTarget{Kind: "agent", Value: "01JBX3K5Q7Z8M4N6P8R0T2V4W1"}
		h.saveRule(t, byMachine)
		h.addReadings(t, h.agentID, "cpu.percent", 95, 5)
		h.addReadings(t, other, "cpu.percent", 95, 5)
		h.evaluate(t)
		list, err := h.service.List(t.Context(), 10)
		if err != nil || len(list) != 2 {
			t.Fatalf("got %+v, %v", list, err)
		}
		got := []string{list[0].AgentName + " " + list[0].RuleName, list[1].AgentName + " " + list[1].RuleName}
		slices.Sort(got)
		if !slices.Equal(got, []string{"nas CPU busy", "web web busy"}) {
			t.Errorf("got %v", got)
		}
	})
}

func TestOfflineRules(t *testing.T) {
	h := newHarness(t)
	phone := saveChannel(t, h, alerts.Channel{Name: "Phone", Type: alerts.ChannelNtfy, Enabled: true,
		Config: alerts.ChannelConfig{Server: h.receiver.url, Topic: "homelab"}})
	rule := h.saveRule(t, alerts.Rule{Name: "Offline", Type: alerts.RuleOffline, Enabled: true,
		Target: alerts.RuleTarget{Kind: "all"}, Config: alerts.RuleConfig{Minutes: 10}, ChannelIDs: []int64{phone.ID}})
	seen := h.clock.Now().UnixMilli()
	h.exec(t, `UPDATE agents SET last_seen_at = ?`, seen)

	h.clock.Advance(5 * time.Minute)
	if got := h.evaluate(t); len(got) != 0 {
		t.Errorf("fired after 5 minutes: %v", got)
	}
	h.clock.Advance(6 * time.Minute)
	if got := h.evaluate(t); !slices.Equal(got, []string{"firing rule." + strconv.FormatInt(rule.ID, 10) + " "}) {
		t.Errorf("got %v", got)
	}
	list, _ := h.service.ForAgent(t.Context(), h.agentID, 10)
	if list[0].Severity != alerts.Critical || list[0].Message != "Offline for more than 10 minutes." {
		t.Errorf("got %+v", list[0])
	}
	h.online[h.agentID] = true
	if got := h.evaluate(t); len(got) != 1 || !strings.HasPrefix(got[0], "resolved") {
		t.Errorf("got %v", got)
	}
	got := h.receiver.take()
	slices.SortFunc(got, func(a, b received) int { return strings.Compare(b.Headers.Get("Priority"), a.Headers.Get("Priority")) })
	if len(got) != 2 || got[0].Headers.Get("Priority") != "5" || got[0].Headers.Get("Title") != "Critical on nas: Offline for more than 10 minutes." ||
		got[1].Headers.Get("Priority") != "3" || got[1].Headers.Get("Title") != "Resolved on nas: Offline for more than 10 minutes." {
		t.Errorf("notified %+v", got)
	}
}

func TestScriptFailedRules(t *testing.T) {
	h := newHarness(t)
	rule := alerts.Rule{Name: "Failed scripts", Type: alerts.RuleScriptFailed, Enabled: true, Target: alerts.RuleTarget{Kind: "all"}}
	rule.ChannelIDs = []int64{saveWebhook(t, h).ID}
	h.saveRule(t, rule)

	if err := h.service.RecordScheduledRun(t.Context(), h.agentID, 7, "backup", "it exited with code 2."); err != nil {
		t.Fatal(err)
	}
	h.service.Wait()
	if got := states(t, h); !slices.Equal(got, []string{"firing script.failed schedule 7"}) {
		t.Errorf("got %v", got)
	}
	if got := h.events(t); !slices.Equal(got, []string{"firing: Scheduled script backup failed: it exited with code 2."}) {
		t.Errorf("notified %v", got)
	}
	// Another schedule's success leaves it firing; this one's resolves it.
	if err := h.service.RecordScheduledRun(t.Context(), h.agentID, 8, "cleanup", ""); err != nil {
		t.Fatal(err)
	}
	if got := states(t, h); !slices.Equal(got, []string{"firing script.failed schedule 7"}) {
		t.Errorf("got %v", got)
	}
	if err := h.service.RecordScheduledRun(t.Context(), h.agentID, 7, "backup", ""); err != nil {
		t.Fatal(err)
	}
	if got := states(t, h); !slices.Equal(got, []string{"resolved script.failed schedule 7"}) {
		t.Errorf("got %v", got)
	}
	if err := h.service.RecordScheduledRun(t.Context(), 999, 7, "backup", "boom"); err != nil {
		t.Errorf("a machine that's gone: %v", err)
	}
}

func TestMaintenance(t *testing.T) {
	h := newHarness(t)
	rule := cpuRule("above", 90)
	rule.ChannelIDs = []int64{saveWebhook(t, h).ID}
	h.saveRule(t, rule)
	h.exec(t, `UPDATE agents SET maintenance_until = ?`, h.clock.Now().Add(time.Hour).UnixMilli())
	h.addReadings(t, h.agentID, "cpu.percent", 95, 5)
	if got := h.evaluate(t); len(got) != 1 || !strings.HasPrefix(got[0], "firing") {
		t.Fatalf("an alert should still be raised in maintenance: %v", got)
	}
	if got := h.events(t); len(got) != 0 {
		t.Errorf("notified in maintenance: %v", got)
	}

	h.clock.Advance(2 * time.Hour)
	h.addReadings(t, h.agentID, "cpu.percent", 10, 0)
	h.evaluate(t)
	if got := h.events(t); !slices.Equal(got, []string{"resolved: CPU has been above 90% for 5 minutes (95%)."}) {
		t.Errorf("after maintenance ended, notified %v", got)
	}
}

func TestAcknowledge(t *testing.T) {
	h := newHarness(t)
	degraded := healthy()
	degraded.Pools[0].Health = "DEGRADED"
	if _, err := h.service.RecordStorageHealth(t.Context(), h.agentID, degraded); err != nil {
		t.Fatal(err)
	}
	list, _ := h.service.ForAgent(t.Context(), h.agentID, 10)
	h.clock.Advance(time.Minute)
	if err := h.service.Acknowledge(t.Context(), h.admin, auth.RequestInfo{}, list[0].ID); err != nil {
		t.Fatal(err)
	}
	list, _ = h.service.ForAgent(t.Context(), h.agentID, 10)
	if list[0].State != alerts.Firing || list[0].AcknowledgedBy != "ada" || list[0].AcknowledgedAt != h.clock.Now().UnixMilli() ||
		list[0].RuleName != "Storage health" {
		t.Errorf("got %+v", list[0])
	}
	if got := auditActions(t, h); !slices.Equal(got, []string{"alert.acknowledge"}) {
		t.Errorf("audited %v", got)
	}

	if _, err := h.service.RecordStorageHealth(t.Context(), h.agentID, healthy()); err != nil {
		t.Fatal(err)
	}
	if err := h.service.Acknowledge(t.Context(), h.admin, auth.RequestInfo{}, list[0].ID); !errors.Is(err, alerts.ErrNotFound) {
		t.Errorf("acknowledging a resolved alert: %v", err)
	}
}

func TestDeleteRuleResolvesItsAlerts(t *testing.T) {
	h := newHarness(t)
	rule := h.saveRule(t, cpuRule("above", 90))
	h.addReadings(t, h.agentID, "cpu.percent", 95, 5)
	h.evaluate(t)
	if err := h.service.DeleteRule(t.Context(), h.admin, auth.RequestInfo{}, rule.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := h.service.ForAgent(t.Context(), h.agentID, 10)
	if len(list) != 1 || list[0].State != alerts.Resolved || list[0].RuleID != nil {
		t.Errorf("got %+v", list)
	}
}

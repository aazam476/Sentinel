// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// webhookReceiver is a webhook endpoint that hands over what it receives.
func webhookReceiver(t *testing.T, status int) (string, <-chan map[string]any) {
	t.Helper()
	got := make(chan map[string]any, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		got <- decoded
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/hook", got
}

// sendCPU sends a metrics sample with the CPU at percent.
func (f terminalFixture) sendCPU(t *testing.T, events <-chan hub.Event, percent float64) {
	t.Helper()
	sample := protocol.MetricsSample{Time: f.server.clock.Now().UnixMilli(), CPUPercent: percent}
	if err := protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeEvent,
		ID: protocol.NewID(f.server.clock.Now()), Action: protocol.ActionMetricsSample}, sample); err != nil {
		t.Fatal(err)
	}
	waitForAgentEvent(t, events, f.agentID, hub.EventMetrics)
}

func TestAlertsAPI(t *testing.T) {
	t.Run("lets viewers see alerts but not manage rules, channels or maintenance", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, viewer.do(http.MethodGet, "/api/alerts", nil), http.StatusOK, "")
		expect(t, viewer.do(http.MethodGet, "/api/alert-rules", nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodGet, "/api/notification-channels", nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodPost, "/api/alerts/1/acknowledge", nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodPut, "/api/agents/"+f.agentID+"/maintenance", api.SetMaintenanceRequest{}), http.StatusForbidden, "forbidden")
	})

	t.Run("a CPU rule fires and resolves, notifying its webhook, and the alert can be acknowledged", func(t *testing.T) {
		f := newTerminalFixture(t)
		hook, received := webhookReceiver(t, http.StatusOK)
		var channel api.NotificationChannel
		saved := f.admin.browser.do(http.MethodPost, "/api/notification-channels", api.SaveNotificationChannelRequest{
			Type: "webhook", Name: "Ops", Enabled: true, Config: api.NotificationChannelConfig{URL: hook},
		})
		expect(t, saved, http.StatusOK, "")
		saved.decode(t, &channel)
		if channel.Config.URL != "" || !channel.HasSecret || channel.Summary != "posts to "+hook[len("http://"):len(hook)-len("/hook")] {
			t.Errorf("channel = %+v", channel)
		}
		var rule api.AlertRule
		f.admin.browser.do(http.MethodPost, "/api/alert-rules", api.SaveAlertRuleRequest{
			Type: "metric", Name: "CPU", Enabled: true, Target: api.AlertRuleTarget{Kind: "all"}, ChannelIDs: []int64{channel.ID},
			Config: api.AlertRuleConfig{Metric: "cpu.percent", Direction: "below", Threshold: 100, Minutes: 1},
		}).decode(t, &rule)
		if rule.ID == 0 || len(rule.ChannelIDs) != 1 {
			t.Fatalf("rule = %+v", rule)
		}

		events, stop := f.server.app.Hub.Subscribe()
		defer stop()
		for range 3 {
			f.sendCPU(t, events, 12)
			f.server.clock.Advance(30 * time.Second)
		}
		if err := f.server.app.Alerts.Evaluate(t.Context()); err != nil {
			t.Fatal(err)
		}
		waitForAgentEvent(t, events, f.agentID, hub.EventAlerts)
		if got := <-received; got["event"] != "firing" || got["message"] != "CPU has been below 100% for 1 minute (12%)." {
			t.Errorf("notified %v", got)
		}
		var list api.FleetAlerts
		f.admin.browser.do(http.MethodGet, "/api/alerts", nil).decode(t, &list)
		if len(list.Alerts) != 1 || list.Alerts[0].AgentID != f.agentID || list.Alerts[0].RuleName != "CPU" || list.Alerts[0].State != "firing" {
			t.Fatalf("alerts = %+v", list)
		}
		alertPath := "/api/alerts/" + strconv.FormatInt(list.Alerts[0].ID, 10) + "/acknowledge"
		expect(t, f.admin.browser.do(http.MethodPost, alertPath, nil), http.StatusNoContent, "")
		f.admin.browser.do(http.MethodGet, "/api/alerts", nil).decode(t, &list)
		if list.Alerts[0].AcknowledgedBy != "admin" || list.Alerts[0].AcknowledgedAt == nil {
			t.Errorf("alerts = %+v", list)
		}

		// "above 100" can't hold, so the alert resolves.
		f.admin.browser.do(http.MethodPut, "/api/alert-rules/"+strconv.FormatInt(rule.ID, 10), api.SaveAlertRuleRequest{
			Type: "metric", Name: "CPU", Enabled: true, Target: api.AlertRuleTarget{Kind: "all"}, ChannelIDs: []int64{channel.ID},
			Config: api.AlertRuleConfig{Metric: "cpu.percent", Direction: "above", Threshold: 100, Minutes: 1},
		}).decode(t, &rule)
		f.sendCPU(t, events, 12)
		if err := f.server.app.Alerts.Evaluate(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := <-received; got["event"] != "resolved" {
			t.Errorf("notified %v", got)
		}
		f.server.app.Alerts.Wait()
		expect(t, f.admin.browser.do(http.MethodPost, alertPath, nil), http.StatusNotFound, "not_found")
	})

	t.Run("lists, tests, changes and deletes rules and channels", func(t *testing.T) {
		f := newTerminalFixture(t)
		working, received := webhookReceiver(t, http.StatusOK)
		broken, _ := webhookReceiver(t, http.StatusInternalServerError)
		var good, bad api.NotificationChannel
		f.admin.browser.do(http.MethodPost, "/api/notification-channels", api.SaveNotificationChannelRequest{
			Type: "webhook", Name: "Good", Enabled: true, Config: api.NotificationChannelConfig{URL: working},
		}).decode(t, &good)
		f.admin.browser.do(http.MethodPost, "/api/notification-channels", api.SaveNotificationChannelRequest{
			Type: "discord", Name: "Bad", Enabled: true, Config: api.NotificationChannelConfig{URL: broken},
		}).decode(t, &bad)
		expect(t, f.admin.browser.do(http.MethodPost, "/api/notification-channels", api.SaveNotificationChannelRequest{
			Type: "webhook", Name: "Good", Config: api.NotificationChannelConfig{URL: working},
		}), http.StatusConflict, "conflict")

		goodPath := "/api/notification-channels/" + strconv.FormatInt(good.ID, 10)
		expect(t, f.admin.browser.do(http.MethodPost, goodPath+"/test", nil), http.StatusNoContent, "")
		if got := <-received; got["event"] != "test" {
			t.Errorf("notified %v", got)
		}
		failed := f.admin.browser.do(http.MethodPost, "/api/notification-channels/"+strconv.FormatInt(bad.ID, 10)+"/test", nil)
		expect(t, failed, http.StatusBadGateway, "channel_failed")
		expect(t, f.admin.browser.do(http.MethodPost, "/api/notification-channels/999/test", nil), http.StatusNotFound, "not_found")

		var channels api.NotificationChannels
		f.admin.browser.do(http.MethodGet, "/api/notification-channels", nil).decode(t, &channels)
		if len(channels.Channels) != 2 || channels.Channels[0].Name != "Bad" || channels.Channels[0].LastError == "" ||
			channels.Channels[1].LastSentAt == nil {
			t.Errorf("channels = %+v", channels)
		}

		// A change without the URL keeps the stored one.
		expect(t, f.admin.browser.do(http.MethodPut, goodPath, api.SaveNotificationChannelRequest{
			Type: "webhook", Name: "Good one", Enabled: true,
		}), http.StatusOK, "")
		expect(t, f.admin.browser.do(http.MethodPost, goodPath+"/test", nil), http.StatusNoContent, "")
		<-received

		invalid := f.admin.browser.do(http.MethodPost, "/api/alert-rules", api.SaveAlertRuleRequest{Type: "metric", Name: "x", Target: api.AlertRuleTarget{Kind: "all"}})
		expect(t, invalid, http.StatusBadRequest, "invalid_input")
		var rules api.AlertRules
		f.admin.browser.do(http.MethodGet, "/api/alert-rules", nil).decode(t, &rules)
		if len(rules.Rules) != 1 || rules.Rules[0].Type != "storage" || rules.Rules[0].Config.CapacityPercent != 80 {
			t.Fatalf("rules = %+v", rules)
		}
		rulePath := "/api/alert-rules/" + strconv.FormatInt(rules.Rules[0].ID, 10)
		expect(t, f.admin.browser.do(http.MethodDelete, rulePath, nil), http.StatusNoContent, "")
		expect(t, f.admin.browser.do(http.MethodDelete, rulePath, nil), http.StatusNotFound, "not_found")
		expect(t, f.admin.browser.do(http.MethodDelete, goodPath, nil), http.StatusNoContent, "")
		expect(t, f.admin.browser.do(http.MethodDelete, goodPath, nil), http.StatusNotFound, "not_found")
		expect(t, f.admin.browser.do(http.MethodDelete, "/api/alert-rules/nope", nil), http.StatusNotFound, "not_found")
	})

	t.Run("puts a machine in maintenance and takes it out", func(t *testing.T) {
		f := newTerminalFixture(t)
		path := "/api/agents/" + f.agentID + "/maintenance"
		until := f.server.clock.Now().Add(time.Hour).UnixMilli()
		var details api.AgentDetails
		got := f.admin.browser.do(http.MethodPut, path, api.SetMaintenanceRequest{Until: &until})
		expect(t, got, http.StatusOK, "")
		got.decode(t, &details)
		if details.MaintenanceUntil == nil || *details.MaintenanceUntil != until {
			t.Errorf("details = %+v", details.MaintenanceUntil)
		}
		var fleet api.AgentsResponse
		f.admin.browser.do(http.MethodGet, "/api/agents", nil).decode(t, &fleet)
		if fleet.Agents[0].MaintenanceUntil == nil {
			t.Error("the fleet doesn't show the maintenance window")
		}
		f.server.clock.Advance(2 * time.Hour)
		f.admin.browser.do(http.MethodGet, "/api/agents", nil).decode(t, &fleet)
		if fleet.Agents[0].MaintenanceUntil != nil {
			t.Error("a window that has ended is still shown")
		}

		past := f.server.clock.Now().UnixMilli()
		expect(t, f.admin.browser.do(http.MethodPut, path, api.SetMaintenanceRequest{Until: &past}), http.StatusBadRequest, "invalid_input")
		f.admin.browser.do(http.MethodPut, path, api.SetMaintenanceRequest{}).decode(t, &details)
		if details.MaintenanceUntil != nil {
			t.Errorf("details = %+v", details.MaintenanceUntil)
		}
		expect(t, f.admin.browser.do(http.MethodPut, "/api/agents/01JBX3K5Q7Z8M4N6P8R0T2V4WM/maintenance", api.SetMaintenanceRequest{}),
			http.StatusNotFound, "not_found")
	})
}

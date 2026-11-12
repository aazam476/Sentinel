// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"

	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// errorCodeChannelFailed: a test notification couldn't be sent.
const errorCodeChannelFailed = "channel_failed"

// resolvedAlertsListed is how many resolved alerts the alerts page lists
// after the firing ones.
const resolvedAlertsListed = 200

// FleetAlert is an alert on any machine, as the alerts page lists it.
type FleetAlert struct {
	MachineAlert `tstype:",extends"`
	AgentID      string `json:"agentId"`
	AgentName    string `json:"agentName"`
}

// FleetAlerts lists every firing alert, then recently resolved ones.
type FleetAlerts struct {
	Alerts []FleetAlert `json:"alerts"`
}

// AlertRuleConfig holds each rule type's settings; see alerts.RuleConfig.
type AlertRuleConfig struct {
	Metric          string  `json:"metric,omitempty"`
	Direction       string  `json:"direction,omitempty"`
	Threshold       float64 `json:"threshold,omitempty"`
	Minutes         int     `json:"minutes,omitempty"`
	CapacityPercent uint64  `json:"capacityPercent,omitempty"`
}

// AlertRuleTarget says which machines a rule watches: kind "all", "tag"
// (value is a tag) or "agent" (a machine's ID).
type AlertRuleTarget struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// AlertRule is one alert rule.
type AlertRule struct {
	ID int64 `json:"id"`
	// Type is metric, offline, storage, script_failed or container.
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Config     AlertRuleConfig `json:"config"`
	Target     AlertRuleTarget `json:"target"`
	Enabled    bool            `json:"enabled"`
	ChannelIDs []int64         `json:"channelIds"`
}

// AlertRules lists every rule.
type AlertRules struct {
	Rules []AlertRule `json:"rules"`
}

// SaveAlertRuleRequest adds or changes a rule.
type SaveAlertRuleRequest struct {
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Config     AlertRuleConfig `json:"config"`
	Target     AlertRuleTarget `json:"target"`
	Enabled    bool            `json:"enabled"`
	ChannelIDs []int64         `json:"channelIds"`
}

// NotificationChannelConfig holds a channel's settings. Secrets (the SMTP
// password, the ntfy token and webhook and Discord URLs) are write-only:
// they are never sent back, and leaving them empty in a change keeps the
// stored ones.
type NotificationChannelConfig struct {
	SMTPHost     string   `json:"smtpHost,omitempty"`
	SMTPPort     int      `json:"smtpPort,omitempty"`
	SMTPUsername string   `json:"smtpUsername,omitempty"`
	SMTPPassword string   `json:"smtpPassword,omitempty"`
	SMTPSecurity string   `json:"smtpSecurity,omitempty"`
	From         string   `json:"from,omitempty"`
	To           []string `json:"to,omitempty"`
	URL          string   `json:"url,omitempty"`
	Server       string   `json:"server,omitempty"`
	Topic        string   `json:"topic,omitempty"`
	Token        string   `json:"token,omitempty"`
}

// NotificationChannel is one way of sending notifications, without its
// secrets.
type NotificationChannel struct {
	ID int64 `json:"id"`
	// Type is email, webhook, ntfy or discord.
	Type    string                    `json:"type"`
	Name    string                    `json:"name"`
	Enabled bool                      `json:"enabled"`
	Config  NotificationChannelConfig `json:"config"`
	// Summary says where it sends, e.g. "posts to discord.com".
	Summary string `json:"summary"`
	// HasSecret says a password, token or URL is stored.
	HasSecret  bool   `json:"hasSecret"`
	LastSentAt *int64 `json:"lastSentAt" tstype:"number | null,required"`
	// LastError is why the last notification failed, or "".
	LastError string `json:"lastError"`
}

// NotificationChannels lists every channel.
type NotificationChannels struct {
	Channels []NotificationChannel `json:"channels"`
}

// SaveNotificationChannelRequest adds or changes a channel.
type SaveNotificationChannelRequest struct {
	Type    string                    `json:"type"`
	Name    string                    `json:"name"`
	Enabled bool                      `json:"enabled"`
	Config  NotificationChannelConfig `json:"config"`
}

// SetMaintenanceRequest puts a machine in maintenance until a time (UTC
// Unix milliseconds), or takes it out with null.
type SetMaintenanceRequest struct {
	Until *int64 `json:"until" tstype:"number | null,required"`
}

// addAlertRoutes adds the alerts page (§9.9): everyone who can see the
// fleet sees alerts, and admins manage rules and channels, acknowledge
// alerts and put machines in maintenance.
func (h *handlers) addAlertRoutes(mux *http.ServeMux) {
	manage := func(handler sessionHandler) http.HandlerFunc {
		return h.withPermission(auth.PermissionManageAlerts, handler)
	}
	mux.HandleFunc("GET /api/alerts", h.withPermission(auth.PermissionViewFleet, h.handleListAlerts))
	mux.HandleFunc("POST /api/alerts/{alert}/acknowledge", manage(h.handleAcknowledgeAlert))
	mux.HandleFunc("GET /api/alert-rules", manage(h.handleListAlertRules))
	mux.HandleFunc("POST /api/alert-rules", manage(h.handleSaveAlertRule))
	mux.HandleFunc("PUT /api/alert-rules/{rule}", manage(h.handleSaveAlertRule))
	mux.HandleFunc("DELETE /api/alert-rules/{rule}", manage(h.handleDeleteAlertRule))
	mux.HandleFunc("GET /api/notification-channels", manage(h.handleListChannels))
	mux.HandleFunc("POST /api/notification-channels", manage(h.handleSaveChannel))
	mux.HandleFunc("PUT /api/notification-channels/{channel}", manage(h.handleSaveChannel))
	mux.HandleFunc("DELETE /api/notification-channels/{channel}", manage(h.handleDeleteChannel))
	mux.HandleFunc("POST /api/notification-channels/{channel}/test", manage(h.handleTestChannel))
	mux.HandleFunc("PUT /api/agents/{id}/maintenance", h.withPermission(auth.PermissionManageMachines, h.handleSetMaintenance))
}

func (h *handlers) handleListAlerts(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	list, err := h.alerts.List(r.Context(), resolvedAlertsListed)
	if err != nil {
		h.writeError(w, err)
		return
	}
	agents, err := h.fleet.AllAgents(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	publicIDs := map[int64]string{}
	for _, agent := range agents {
		publicIDs[agent.ID] = agent.PublicID
	}
	response := FleetAlerts{Alerts: make([]FleetAlert, 0, len(list))}
	for _, alert := range list {
		publicID, active := publicIDs[alert.AgentID]
		if !active {
			continue // a revoked machine's alerts aren't shown
		}
		response.Alerts = append(response.Alerts, FleetAlert{MachineAlert: machineAlert(alert), AgentID: publicID, AgentName: alert.AgentName})
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleAcknowledgeAlert(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "alert")
	if !ok {
		return
	}
	if err := h.alerts.Acknowledge(r.Context(), session, requestInfo(r), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func ruleView(rule alerts.Rule) AlertRule {
	return AlertRule{
		ID: rule.ID, Type: rule.Type, Name: rule.Name, Config: AlertRuleConfig(rule.Config),
		Target: AlertRuleTarget(rule.Target), Enabled: rule.Enabled, ChannelIDs: rule.ChannelIDs,
	}
}

func (h *handlers) handleListAlertRules(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	rules, err := h.alerts.Rules(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := AlertRules{Rules: make([]AlertRule, 0, len(rules))}
	for _, rule := range rules {
		response.Rules = append(response.Rules, ruleView(rule))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleSaveAlertRule(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "rule")
	if !ok {
		return
	}
	var request SaveAlertRuleRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	channelIDs := request.ChannelIDs
	if channelIDs == nil {
		channelIDs = []int64{}
	}
	saved, err := h.alerts.SaveRule(r.Context(), session, requestInfo(r), alerts.Rule{
		ID: id, Type: request.Type, Name: request.Name, Config: alerts.RuleConfig(request.Config),
		Target: alerts.RuleTarget(request.Target), Enabled: request.Enabled, ChannelIDs: channelIDs,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ruleView(saved))
}

func (h *handlers) handleDeleteAlertRule(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "rule")
	if !ok {
		return
	}
	if err := h.alerts.DeleteRule(r.Context(), session, requestInfo(r), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// channelView is a channel without its secrets (SEC-42).
func channelView(channel alerts.Channel) NotificationChannel {
	config := NotificationChannelConfig(channel.Config)
	hasSecret := config.SMTPPassword != "" || config.Token != "" || config.URL != ""
	config.SMTPPassword, config.Token, config.URL = "", "", ""
	return NotificationChannel{
		ID: channel.ID, Type: channel.Type, Name: channel.Name, Enabled: channel.Enabled, Config: config,
		Summary: channel.Summary(), HasSecret: hasSecret, LastSentAt: channel.LastSentAt, LastError: channel.LastError,
	}
}

func (h *handlers) handleListChannels(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	channels, err := h.alerts.Channels(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := NotificationChannels{Channels: make([]NotificationChannel, 0, len(channels))}
	for _, channel := range channels {
		response.Channels = append(response.Channels, channelView(channel))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleSaveChannel(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "channel")
	if !ok {
		return
	}
	var request SaveNotificationChannelRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	saved, err := h.alerts.SaveChannel(r.Context(), session, requestInfo(r), alerts.Channel{
		ID: id, Type: request.Type, Name: request.Name, Enabled: request.Enabled, Config: alerts.ChannelConfig(request.Config),
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, channelView(saved))
}

func (h *handlers) handleDeleteChannel(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "channel")
	if !ok {
		return
	}
	if err := h.alerts.DeleteChannel(r.Context(), session, requestInfo(r), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestChannel sends a test notification. A channel that can't send
// is not a server error: the reason goes back to the admin as a 502.
func (h *handlers) handleTestChannel(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "channel")
	if !ok {
		return
	}
	err := h.alerts.TestChannel(r.Context(), session, id)
	var failed *alerts.DeliveryError
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.As(err, &failed):
		writeError(w, http.StatusBadGateway, errorCodeChannelFailed, "The test notification wasn't sent: "+failed.Err.Error())
	default:
		h.writeError(w, err)
	}
}

func (h *handlers) handleSetMaintenance(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request SetMaintenanceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	agent, err := h.fleet.SetMaintenance(r.Context(), session, r.PathValue("id"), request.Until, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	counts, err := h.alerts.FiringCounts(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	details, err := h.withUpdateStatus(r.Context(), h.agentDetails(agent, counts), agent.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

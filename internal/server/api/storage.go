// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// resolvedAlertsShown is how many resolved alerts a machine's alert list
// includes after the firing ones.
const resolvedAlertsShown = 50

// MachineAlert is one alert on a machine.
type MachineAlert struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	State    string `json:"state"`
	// StartedAt and ResolvedAt are UTC Unix milliseconds.
	StartedAt  int64  `json:"startedAt"`
	ResolvedAt *int64 `json:"resolvedAt" tstype:"number | null,required"`
}

// MachineAlerts lists a machine's firing alerts, then its recently
// resolved ones.
type MachineAlerts struct {
	Alerts []MachineAlert `json:"alerts"`
}

// addStorageRoutes adds the storage view (§9.5), which everyone who can
// see the fleet can read, and machine alerts.
func (h *handlers) addStorageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents/{id}/storage", h.withPermission(auth.PermissionViewFleet, h.handleStorageOverview))
	mux.HandleFunc("GET /api/agents/{id}/storage/pools/{pool}", h.withPermission(auth.PermissionViewFleet, h.handlePoolStatus))
	mux.HandleFunc("GET /api/agents/{id}/alerts", h.withPermission(auth.PermissionViewFleet, h.handleMachineAlerts))
}

func (h *handlers) handleStorageOverview(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var overview protocol.StorageOverviewPayload
	if err := h.ask(r.Context(), agent, protocol.ActionStorageOverview, nil, &overview); err != nil {
		h.writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (h *handlers) handlePoolStatus(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var pool protocol.ZFSPool
	request := protocol.ZFSPoolStatusPayload{Pool: r.PathValue("pool")}
	if err := h.ask(r.Context(), agent, protocol.ActionZFSPoolStatus, request, &pool); err != nil {
		h.writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pool)
}

// handleMachineAlerts lists a machine's alerts. They are kept while it is
// offline, so this works whether it is online or not.
func (h *handlers) handleMachineAlerts(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, err := h.fleet.GetAgent(r.Context(), session, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	list, err := h.alerts.ForAgent(r.Context(), agent.ID, resolvedAlertsShown)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := MachineAlerts{Alerts: make([]MachineAlert, 0, len(list))}
	for _, alert := range list {
		response.Alerts = append(response.Alerts, machineAlert(alert))
	}
	writeJSON(w, http.StatusOK, response)
}

func machineAlert(alert alerts.Alert) MachineAlert {
	result := MachineAlert{
		ID: alert.ID, Kind: alert.Kind, Subject: alert.Subject, Severity: alert.Severity, Message: alert.Message,
		State: alert.State, StartedAt: alert.StartedAt,
	}
	if alert.ResolvedAt != 0 {
		resolvedAt := alert.ResolvedAt
		result.ResolvedAt = &resolvedAt
	}
	return result
}

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// MachineService is one service on a machine.
type MachineService struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// State is running, stopped, failed, starting, stopping or unknown.
	State string `json:"state" tstype:"'running' | 'stopped' | 'failed' | 'starting' | 'stopping' | 'unknown'"`
	// Startup is automatic, manual, disabled or fixed (the service manager
	// decides, so enable and disable don't apply).
	Startup string `json:"startup" tstype:"'automatic' | 'manual' | 'disabled' | 'fixed'"`
}

// ServiceList answers GET /api/agents/{id}/services.
type ServiceList struct {
	// Manager is systemd, launchd or scm.
	Manager  string           `json:"manager" tstype:"'systemd' | 'launchd' | 'scm'"`
	Services []MachineService `json:"services"`
}

// MachineProcess is one process on a machine.
type MachineProcess struct {
	PID  int32  `json:"pid"`
	Name string `json:"name"`
	User string `json:"user"`
	// CPUPercent is use over the last second; 100 is one whole core.
	CPUPercent  float64 `json:"cpuPercent"`
	MemoryBytes uint64  `json:"memoryBytes"`
	Command     string  `json:"command"`
}

// ProcessList answers GET /api/agents/{id}/processes.
type ProcessList struct {
	Processes []MachineProcess `json:"processes"`
}

// KillProcessRequest is the body of POST /api/agents/{id}/processes/{pid}/kill.
type KillProcessRequest struct {
	// Force kills outright instead of asking the process to stop.
	Force bool `json:"force"`
}

// MachineLogEntry is one line of a machine's log.
type MachineLogEntry struct {
	// Time is UTC Unix milliseconds.
	Time int64 `json:"time"`
	// Level is error, warning, notice, info or debug.
	Level   string `json:"level" tstype:"'error' | 'warning' | 'notice' | 'info' | 'debug'"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

// LogEntries answers GET /api/agents/{id}/logs, newest first.
type LogEntries struct {
	Entries []MachineLogEntry `json:"entries"`
	// Truncated is true when there were more entries than the limit.
	Truncated bool `json:"truncated"`
}

// PowerRequest is the body of POST /api/agents/{id}/power.
type PowerRequest struct {
	// Action is reboot or shutdown.
	Action string `json:"action" tstype:"'reboot' | 'shutdown'"`
}

func (h *handlers) handleListServices(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var list protocol.ServiceListPayload
	if err := h.ask(r.Context(), agent, protocol.ActionServiceList, nil, &list); err != nil {
		h.writeAgentError(w, err)
		return
	}
	response := ServiceList{Manager: list.Manager, Services: make([]MachineService, 0, len(list.Services))}
	for _, service := range list.Services {
		response.Services = append(response.Services, MachineService{Name: service.Name, Description: service.Description,
			State: string(service.State), Startup: string(service.Startup)})
	}
	writeJSON(w, http.StatusOK, response)
}

// serviceActions maps the path's action to the agent's.
func serviceActions() map[string]string {
	return map[string]string{
		"start": protocol.ActionServiceStart, "stop": protocol.ActionServiceStop, "restart": protocol.ActionServiceRestart,
		"enable": protocol.ActionServiceEnable, "disable": protocol.ActionServiceDisable,
	}
}

// handleServiceAction is POST /api/agents/{id}/services/{name}/{action}.
func (h *handlers) handleServiceAction(w http.ResponseWriter, r *http.Request, session auth.Session) {
	action, known := serviceActions()[r.PathValue("action")]
	if !known {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "There is no API endpoint at this address.")
		return
	}
	name := r.PathValue("name")
	if name == "" || len(name) > 256 {
		writeInvalidInput(w, "name", "That isn’t a valid service name.")
		return
	}
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	if err := h.doAudited(r, session, agent, action, protocol.ServiceActionPayload{Name: name}, map[string]any{"service": name}); err != nil {
		h.writeAgentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) handleListProcesses(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var list protocol.ProcessListPayload
	if err := h.ask(r.Context(), agent, protocol.ActionProcessList, nil, &list); err != nil {
		h.writeAgentError(w, err)
		return
	}
	response := ProcessList{Processes: make([]MachineProcess, 0, len(list.Processes))}
	for _, process := range list.Processes {
		response.Processes = append(response.Processes, MachineProcess(process))
	}
	writeJSON(w, http.StatusOK, response)
}

// handleKillProcess is POST /api/agents/{id}/processes/{pid}/kill.
func (h *handlers) handleKillProcess(w http.ResponseWriter, r *http.Request, session auth.Session) {
	pid, err := strconv.ParseInt(r.PathValue("pid"), 10, 32)
	if err != nil || pid < 1 {
		writeInvalidInput(w, "pid", "That isn’t a process ID.")
		return
	}
	var request KillProcessRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	payload := protocol.ProcessKillPayload{PID: int32(pid), Force: request.Force}
	if err := h.doAudited(r, session, agent, protocol.ActionProcessKill, payload, map[string]any{"pid": pid, "force": request.Force}); err != nil {
		h.writeAgentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePower is POST /api/agents/{id}/power. The agent answers before the
// machine goes down; the UI then waits for it to come back.
func (h *handlers) handlePower(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request PowerRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	action := map[string]string{"reboot": protocol.ActionSystemReboot, "shutdown": protocol.ActionSystemShutdown}[request.Action]
	if action == "" {
		writeInvalidInput(w, "action", "The action must be reboot or shutdown.")
		return
	}
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	if !allowsCapability(agent, "power") {
		writeError(w, http.StatusForbidden, errorCodePolicyDenied, "Rebooting and shutting down are disabled by this machine’s local policy.")
		return
	}
	if err := h.doAudited(r, session, agent, action, nil, map[string]any{}); err != nil {
		h.writeAgentError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleQueryLogs is GET /api/agents/{id}/logs?service=&since=&level=&limit=.
func (h *handlers) handleQueryLogs(w http.ResponseWriter, r *http.Request, session auth.Session) {
	query, ok := logQuery(w, r)
	if !ok {
		return
	}
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	action := protocol.ActionLogQuery
	if query.Service != "" {
		action = protocol.ActionServiceLogs
	}
	var entries protocol.LogEntriesPayload
	if err := h.ask(r.Context(), agent, action, query, &entries); err != nil {
		h.writeAgentError(w, err)
		return
	}
	response := LogEntries{Entries: make([]MachineLogEntry, 0, len(entries.Entries)), Truncated: entries.Truncated}
	for _, entry := range entries.Entries {
		response.Entries = append(response.Entries, MachineLogEntry{Time: entry.Time, Level: string(entry.Level), Source: entry.Source, Message: entry.Message})
	}
	writeJSON(w, http.StatusOK, response)
}

// logQuery reads the query string, writing the error response when it is
// invalid.
func logQuery(w http.ResponseWriter, r *http.Request) (protocol.LogQueryPayload, bool) {
	values := r.URL.Query()
	query := protocol.LogQueryPayload{Service: values.Get("service"), MinLevel: protocol.LogLevel(values.Get("level"))}
	levels := []protocol.LogLevel{"", protocol.LogError, protocol.LogWarning, protocol.LogNotice, protocol.LogInfo, protocol.LogDebug}
	if !slices.Contains(levels, query.MinLevel) {
		writeInvalidInput(w, "level", "The level must be error, warning, notice, info or debug.")
		return query, false
	}
	if len(query.Service) > 256 {
		writeInvalidInput(w, "service", "That isn’t a valid service name.")
		return query, false
	}
	if text := values.Get("since"); text != "" {
		since, err := strconv.ParseInt(text, 10, 64)
		if err != nil || since < 0 {
			writeInvalidInput(w, "since", "since must be a time in Unix milliseconds.")
			return query, false
		}
		query.Since = since
	}
	if text := values.Get("limit"); text != "" {
		limit, err := strconv.Atoi(text)
		if err != nil || limit < 1 || limit > 1000 {
			writeInvalidInput(w, "limit", "The limit must be 1 to 1,000.")
			return query, false
		}
		query.Limit = limit
	}
	return query, true
}

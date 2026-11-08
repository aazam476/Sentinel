// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/metrics"
)

// Agent is a managed machine as the fleet view shows it.
type Agent struct {
	// ID is the agent's ULID.
	ID string `json:"id"`
	// Name is the display name, or the hostname when there is none.
	Name            string   `json:"name"`
	DisplayName     string   `json:"displayName"`
	Hostname        string   `json:"hostname"`
	OS              string   `json:"os"`
	Platform        string   `json:"platform"`
	PlatformVersion string   `json:"platformVersion"`
	Arch            string   `json:"arch"`
	AgentVersion    string   `json:"agentVersion"`
	Online          bool     `json:"online"`
	LastSeenAt      *int64   `json:"lastSeenAt" tstype:"number | null,required"`
	EnrolledAt      int64    `json:"enrolledAt"`
	Tags            []string `json:"tags"`
	// Current is the newest sample, while the machine is online.
	Current *CurrentMetrics `json:"current" tstype:"CurrentMetrics | null,required"`
	// FiringAlerts counts the machine's alerts that are firing.
	FiringAlerts int `json:"firingAlerts"`
}

// CurrentMetrics is a machine's newest metrics sample.
type CurrentMetrics struct {
	Time          int64  `json:"time"`
	UptimeSeconds uint64 `json:"uptimeSeconds"`
	// Values holds each metric by name (see internal/server/metrics). A
	// metric the machine doesn't report, or a rate right after it
	// connected, is missing.
	Values map[string]float64 `json:"values"`
}

// AgentDetails is everything the machine view shows about one machine.
type AgentDetails struct {
	Agent            `tstype:",extends"`
	KernelVersion    string       `json:"kernelVersion"`
	CPUModel         string       `json:"cpuModel"`
	CPUCores         int          `json:"cpuCores"`
	MemoryTotalBytes uint64       `json:"memoryTotalBytes"`
	Capabilities     []Capability `json:"capabilities"`
	// Filesystems and Temperatures come from the newest sample; both are
	// empty while the machine is offline.
	Filesystems  []Filesystem  `json:"filesystems"`
	Temperatures []Temperature `json:"temperatures"`
}

// Capability is a feature and whether the machine's local policy allows it.
type Capability struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

// Filesystem is one mounted filesystem's usage.
type Filesystem struct {
	Mount      string `json:"mount"`
	Type       string `json:"type"`
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

// Temperature is one sensor reading.
type Temperature struct {
	Sensor  string  `json:"sensor"`
	Celsius float64 `json:"celsius"`
}

// AgentsResponse lists every machine that hasn't been revoked, by name.
type AgentsResponse struct {
	Agents []Agent `json:"agents"`
}

// UpdateAgentRequest renames or re-tags a machine. Missing fields are left
// as they are; an empty display name goes back to the hostname.
type UpdateAgentRequest struct {
	DisplayName *string   `json:"displayName"`
	Tags        *[]string `json:"tags"`
}

func (h *handlers) handleListAgents(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agents, err := h.fleet.ListAgents(r.Context(), session)
	if err != nil {
		h.writeError(w, err)
		return
	}
	counts, err := h.alerts.FiringCounts(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := AgentsResponse{Agents: make([]Agent, 0, len(agents))}
	for _, agent := range agents {
		response.Agents = append(response.Agents, h.agentSummary(agent, counts))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleGetAgent(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, err := h.fleet.GetAgent(r.Context(), session, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeAgentDetails(w, r, agent)
}

func (h *handlers) handleUpdateAgent(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request UpdateAgentRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	agent, err := h.fleet.UpdateAgent(r.Context(), session, r.PathValue("id"),
		fleet.UpdateAgentInput{DisplayName: request.DisplayName, Tags: request.Tags}, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeAgentDetails(w, r, agent)
}

// handleRevokeAgent revokes a machine and closes its live connection
// (SEC-21). The order matters: see hub.Hub.Disconnect.
func (h *handlers) handleRevokeAgent(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, err := h.fleet.RevokeAgent(r.Context(), session, r.PathValue("id"), requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.hub.Disconnect(agent.ID)
	h.metrics.Forget(agent.ID)
	w.WriteHeader(http.StatusNoContent)
}

// writeAgentDetails answers with one machine's details.
func (h *handlers) writeAgentDetails(w http.ResponseWriter, r *http.Request, agent fleet.Agent) {
	counts, err := h.alerts.FiringCounts(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.agentDetails(agent, counts))
}

func (h *handlers) agentSummary(agent fleet.Agent, firingAlerts map[int64]int) Agent {
	summary := Agent{
		ID: agent.PublicID, Name: agent.Name(), DisplayName: agent.DisplayName, Hostname: agent.Hostname,
		OS: agent.OS, Platform: agent.SystemInfo.Platform, PlatformVersion: agent.SystemInfo.PlatformVersion,
		Arch: agent.Arch, AgentVersion: agent.AgentVersion, Online: h.hub.IsOnline(agent.ID),
		LastSeenAt: agent.LastSeenAt, EnrolledAt: agent.EnrolledAt, Tags: agent.Tags, FiringAlerts: firingAlerts[agent.ID],
	}
	if snapshot, ok := h.metrics.Latest(agent.ID); ok && summary.Online {
		summary.Current = currentMetrics(snapshot)
	}
	return summary
}

func (h *handlers) agentDetails(agent fleet.Agent, firingAlerts map[int64]int) AgentDetails {
	details := AgentDetails{
		Agent: h.agentSummary(agent, firingAlerts), KernelVersion: agent.SystemInfo.KernelVersion,
		CPUModel: agent.SystemInfo.CPUModel, CPUCores: agent.SystemInfo.CPUCores,
		MemoryTotalBytes: agent.SystemInfo.MemoryTotal,
		Capabilities:     capabilities(agent.SystemInfo.Capabilities),
		Filesystems:      []Filesystem{}, Temperatures: []Temperature{},
	}
	if snapshot, ok := h.metrics.Latest(agent.ID); ok && details.Online {
		for _, filesystem := range snapshot.Filesystems {
			details.Filesystems = append(details.Filesystems, Filesystem(filesystem))
		}
		for _, temperature := range snapshot.Temperatures {
			details.Temperatures = append(details.Temperatures, Temperature(temperature))
		}
	}
	return details
}

func currentMetrics(snapshot metrics.Snapshot) *CurrentMetrics {
	return &CurrentMetrics{Time: snapshot.Time, UptimeSeconds: snapshot.UptimeSeconds, Values: snapshot.Values}
}

func capabilities(reported []protocol.Capability) []Capability {
	result := make([]Capability, 0, len(reported))
	for _, capability := range reported {
		result = append(result, Capability(capability))
	}
	return result
}

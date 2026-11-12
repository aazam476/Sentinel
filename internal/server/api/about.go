// SPDX-License-Identifier: Apache-2.0

package api

import (
	"cmp"
	"net/http"
	"slices"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
)

// AboutResponse describes the server and its fleet (§9.11).
type AboutResponse struct {
	ServerVersion string `json:"serverVersion"`
	// DatabaseBytes is the size of the server's database.
	DatabaseBytes int64 `json:"databaseBytes"`
	// Machines counts the enrolled machines.
	Machines int `json:"machines"`
	// AgentVersions is how many machines run each agent version, most
	// common first.
	AgentVersions []AgentVersionCount `json:"agentVersions"`
}

// AgentVersionCount is how many machines run one agent version.
type AgentVersionCount struct {
	Version  string `json:"version"`
	Machines int    `json:"machines"`
}

// handleAbout is GET /api/about.
func (h *handlers) handleAbout(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agents, err := h.fleet.ListAgents(r.Context(), session)
	if err != nil {
		h.writeError(w, err)
		return
	}
	size, err := h.database.SizeBytes(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	versions := make([]string, 0, len(agents))
	for _, agent := range agents {
		versions = append(versions, agent.AgentVersion)
	}
	writeJSON(w, http.StatusOK, AboutResponse{
		ServerVersion: buildinfo.Version(), DatabaseBytes: size, Machines: len(agents), AgentVersions: countVersions(versions),
	})
}

// countVersions counts each version, most common first, then by name.
func countVersions(versions []string) []AgentVersionCount {
	counts := map[string]int{}
	for _, version := range versions {
		if version == "" {
			version = "unknown"
		}
		counts[version]++
	}
	result := make([]AgentVersionCount, 0, len(counts))
	for version, machines := range counts {
		result = append(result, AgentVersionCount{Version: version, Machines: machines})
	}
	slices.SortFunc(result, func(a, b AgentVersionCount) int {
		return cmp.Or(cmp.Compare(b.Machines, a.Machines), cmp.Compare(a.Version, b.Version))
	})
	return result
}

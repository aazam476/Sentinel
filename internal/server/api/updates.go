// SPDX-License-Identifier: Apache-2.0

package api

import (
	"io"
	"net/http"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/updates"
)

// maxBuildUpload bounds an uploaded agent build and its signature.
const maxBuildUpload = 600 << 20

// AgentBuild is one hosted agent build.
type AgentBuild struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	AddedAt int64  `json:"addedAt"`
}

// AgentBuilds lists the hosted builds, newest first, and the update settings.
type AgentBuilds struct {
	Builds     []AgentBuild `json:"builds"`
	AutoUpdate bool         `json:"autoUpdate"`
	// CanFetch says a release channel is set, so builds can be fetched.
	CanFetch bool `json:"canFetch"`
	// DevelopmentKey says this server trusts only the development release
	// key: fine for testing, never for real machines.
	DevelopmentKey bool `json:"developmentKey"`
}

// FetchedBuilds lists the builds a fetch added.
type FetchedBuilds struct {
	Builds []AgentBuild `json:"builds"`
}

// SetAutoUpdateRequest turns automatic updates on or off.
type SetAutoUpdateRequest struct {
	Enabled bool `json:"enabled"`
}

// UpdateAgentsRequest updates machines: target kind "all", "tag" or
// "agent"; version "" means the newest build.
type UpdateAgentsRequest struct {
	Target  ScriptTarget `json:"target"`
	Version string       `json:"version"`
}

// UpdateAgentsResponse says which machines started updating, and why the
// others didn't.
type UpdateAgentsResponse struct {
	Started []string          `json:"started"`
	Skipped map[string]string `json:"skipped"`
}

// AgentUpdateStatus is the last update a machine was asked to make.
type AgentUpdateStatus struct {
	Version string `json:"version"`
	// State is updating, updated, failed or rolled_back.
	State      string `json:"state"`
	Error      string `json:"error"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt *int64 `json:"finishedAt" tstype:"number | null,required"`
}

// addUpdateRoutes adds agent builds and updates (§11.3), for admins, and
// the builds themselves, for anyone.
func (h *handlers) addUpdateRoutes(mux *http.ServeMux) {
	manage := func(handler sessionHandler) http.HandlerFunc {
		return h.withPermission(auth.PermissionManageMachines, handler)
	}
	mux.HandleFunc("GET /api/agent-builds", manage(h.handleListBuilds))
	mux.HandleFunc("POST /api/agent-builds", manage(h.handleUploadBuild))
	mux.HandleFunc("POST /api/agent-builds/fetch", manage(h.handleFetchBuilds))
	mux.HandleFunc("PUT /api/agent-builds/auto-update", manage(h.handleSetAutoUpdate))
	mux.HandleFunc("POST /api/agent-updates", manage(h.handleUpdateAgents))
	mux.Handle("GET /install/updates/{version}/{file}", h.updates.Handler())
}

func buildView(build updates.Build) AgentBuild {
	return AgentBuild{Version: build.Version, OS: build.OS, Arch: build.Arch, File: build.File, Size: build.Size, AddedAt: build.AddedAt}
}

func (h *handlers) handleListBuilds(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	builds, err := h.updates.Builds()
	if err != nil {
		h.writeError(w, err)
		return
	}
	auto, err := h.updates.AutoUpdate(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := AgentBuilds{Builds: make([]AgentBuild, 0, len(builds)), AutoUpdate: auto, CanFetch: h.updates.CanFetch(), DevelopmentKey: updates.IsDevelopmentKey()}
	for _, build := range builds {
		response.Builds = append(response.Builds, buildView(build))
	}
	writeJSON(w, http.StatusOK, response)
}

// handleUploadBuild takes a build and its signature as multipart form
// fields "build" and "signature".
func (h *handlers) handleUploadBuild(w http.ResponseWriter, r *http.Request, session auth.Session) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBuildUpload)
	reader, err := r.MultipartReader()
	if err != nil {
		writeInvalidInput(w, "build", "Send the build and its signature as a form.")
		return
	}
	var build io.Reader
	var signature []byte
	for build == nil {
		part, err := reader.NextPart()
		if err != nil {
			writeInvalidInput(w, "build", "Send the signature, then the build.")
			return
		}
		switch part.FormName() {
		case "signature":
			if signature, err = io.ReadAll(io.LimitReader(part, 16<<10)); err != nil {
				writeInvalidInput(w, "signature", "The signature couldn't be read.")
				return
			}
		case "build":
			build = part
		}
	}
	added, err := h.updates.Add(r.Context(), session, requestInfo(r), build, signature)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, buildView(added))
}

func (h *handlers) handleFetchBuilds(w http.ResponseWriter, r *http.Request, session auth.Session) {
	added, err := h.updates.Fetch(r.Context(), session, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := FetchedBuilds{Builds: make([]AgentBuild, 0, len(added))}
	for _, build := range added {
		response.Builds = append(response.Builds, buildView(build))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleSetAutoUpdate(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request SetAutoUpdateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.updates.SetAutoUpdate(r.Context(), session, requestInfo(r), request.Enabled); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) handleUpdateAgents(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request UpdateAgentsRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	outcome, err := h.updates.Update(r.Context(), session, requestInfo(r), updates.Target(request.Target), request.Version)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, UpdateAgentsResponse{Started: outcome.Started, Skipped: outcome.Skipped})
}

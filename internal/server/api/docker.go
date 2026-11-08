// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// Time limits for Docker actions that can take minutes.
const (
	// pullTimeout covers pulling an image, or a stack's images and
	// redeploying it.
	pullTimeout = 15 * time.Minute
	// deployTimeout covers starting or stopping a stack.
	deployTimeout = 5 * time.Minute
)

// DockerImageUpdate is the last update check of an image tag.
type DockerImageUpdate struct {
	UpdateAvailable bool `json:"updateAvailable"`
	// Error says why the last check failed, or is empty.
	Error     string `json:"error"`
	CheckedAt int64  `json:"checkedAt"`
}

// DockerImages answers GET /api/agents/{id}/docker/images: the images and,
// by tag, what the last update check found.
type DockerImages struct {
	Images  []protocol.Image             `json:"images"`
	Updates map[string]DockerImageUpdate `json:"updates"`
}

// ImageReferenceRequest names an image to pull or check.
type ImageReferenceRequest struct {
	Reference string `json:"reference"`
}

// SaveStackRequest is the body of PUT /api/agents/{id}/docker/stacks/{name}.
type SaveStackRequest struct {
	Compose string `json:"compose"`
	Env     string `json:"env"`
	// ValidateOnly checks the files with `docker compose config` without
	// saving them.
	ValidateOnly bool `json:"validateOnly"`
}

// dockerAction is one state-changing Docker request: what to send the
// agent, how long it may take, and what the audit log records.
type dockerAction struct {
	action  string
	payload any
	timeout time.Duration
	details map[string]any
}

// doDocker runs a Docker action on the machine in the path and audits it,
// writing the response: 204, or the error.
func (h *handlers) doDocker(w http.ResponseWriter, r *http.Request, session auth.Session, request dockerAction) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	if request.timeout == 0 {
		request.timeout = time.Minute
	}
	ask := func(ctx context.Context) error {
		return h.hub.RequestWithin(ctx, agent.ID, request.timeout, request.action, request.payload, nil)
	}
	if err := h.audited(r, session, agent, request.action, request.details, ask); err != nil {
		h.writeAgentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getDocker asks the machine in the path for something and writes the
// agent's answer as it is.
func (h *handlers) getDocker(w http.ResponseWriter, r *http.Request, session auth.Session, action string, payload, result any) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	if err := h.ask(r.Context(), agent, action, payload, result); err != nil {
		h.writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// dockerList returns a handler for a list with no payload.
func dockerList[T any](h *handlers, action string) func(http.ResponseWriter, *http.Request, auth.Session) {
	return func(w http.ResponseWriter, r *http.Request, session auth.Session) {
		var result T
		h.getDocker(w, r, session, action, nil, &result)
	}
}

// containerActions maps the path's action to the agent's.
func containerActions() map[string]string {
	return map[string]string{"start": protocol.ActionDockerContainerStart, "stop": protocol.ActionDockerContainerStop,
		"restart": protocol.ActionDockerContainerRestart}
}

func (h *handlers) handleContainerAction(w http.ResponseWriter, r *http.Request, session auth.Session) {
	action, known := containerActions()[r.PathValue("action")]
	if !known {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "There is no API endpoint at this address.")
		return
	}
	container := r.PathValue("container")
	h.doDocker(w, r, session, dockerAction{action: action, payload: protocol.ContainerPayload{ID: container},
		details: map[string]any{"container": container}})
}

func (h *handlers) handleRemoveContainer(w http.ResponseWriter, r *http.Request, session auth.Session) {
	container, force := r.PathValue("container"), r.URL.Query().Get("force") == "true"
	h.doDocker(w, r, session, dockerAction{action: protocol.ActionDockerContainerRemove,
		payload: protocol.ContainerPayload{ID: container, Force: force}, details: map[string]any{"container": container, "force": force}})
}

func (h *handlers) handleInspectContainer(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var result protocol.ContainerInspectPayload
	h.getDocker(w, r, session, protocol.ActionDockerContainerInspect, protocol.ContainerPayload{ID: r.PathValue("container")}, &result)
}

// containerLogsRequest reads ?tail= and ?since=, writing the error when
// they are invalid.
func containerLogsRequest(w http.ResponseWriter, r *http.Request) (protocol.ContainerLogsPayload, bool) {
	request := protocol.ContainerLogsPayload{ID: r.PathValue("container")}
	query := r.URL.Query()
	if text := query.Get("tail"); text != "" {
		tail, err := strconv.Atoi(text)
		if err != nil || tail < 1 || tail > 5000 {
			writeInvalidInput(w, "tail", "tail must be 1 to 5,000.")
			return request, false
		}
		request.Tail = tail
	}
	if text := query.Get("since"); text != "" {
		since, err := strconv.ParseInt(text, 10, 64)
		if err != nil || since < 0 {
			writeInvalidInput(w, "since", "since must be a time in Unix milliseconds.")
			return request, false
		}
		request.Since = since
	}
	return request, true
}

func (h *handlers) handleContainerLogs(w http.ResponseWriter, r *http.Request, session auth.Session) {
	request, ok := containerLogsRequest(w, r)
	if !ok {
		return
	}
	var result protocol.ContainerLogsResultPayload
	h.getDocker(w, r, session, protocol.ActionDockerContainerLogs, request, &result)
}

func (h *handlers) handleListImages(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var images protocol.ImageListPayload
	if err := h.ask(r.Context(), agent, protocol.ActionDockerImageList, nil, &images); err != nil {
		h.writeAgentError(w, err)
		return
	}
	h.writeImages(w, r.Context(), agent, images.Images)
}

func (h *handlers) writeImages(w http.ResponseWriter, ctx context.Context, agent fleet.Agent, images []protocol.Image) {
	checks, err := h.registries.Updates(ctx, agent.ID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := DockerImages{Images: images, Updates: map[string]DockerImageUpdate{}}
	for reference, check := range checks {
		response.Updates[reference] = DockerImageUpdate{UpdateAvailable: check.UpdateAvailable, Error: check.Error, CheckedAt: check.CheckedAt}
	}
	writeJSON(w, http.StatusOK, response)
}

// handlePullImage pulls an image with the stored login for its registry,
// if there is one.
func (h *handlers) handlePullImage(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request ImageReferenceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	login, err := h.registries.AuthFor(r.Context(), request.Reference)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.doDocker(w, r, session, dockerAction{action: protocol.ActionDockerImagePull, timeout: pullTimeout,
		payload: protocol.ImageRefPayload{Reference: request.Reference, Auth: login},
		details: map[string]any{"reference": request.Reference, "withLogin": login != nil}})
}

// handleCheckImages checks every tagged image on the machine for updates
// now, rather than waiting for the daily check.
func (h *handlers) handleCheckImages(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	if _, err := h.registries.CheckMachine(r.Context(), h.hub, agent.ID); err != nil {
		h.writeAgentError(w, err)
		return
	}
	var images protocol.ImageListPayload
	if err := h.ask(r.Context(), agent, protocol.ActionDockerImageList, nil, &images); err != nil {
		h.writeAgentError(w, err)
		return
	}
	h.writeImages(w, r.Context(), agent, images.Images)
}

func (h *handlers) handleRemoveImage(w http.ResponseWriter, r *http.Request, session auth.Session) {
	reference, force := r.URL.Query().Get("reference"), r.URL.Query().Get("force") == "true"
	h.doDocker(w, r, session, dockerAction{action: protocol.ActionDockerImageRemove,
		payload: protocol.ImageRefPayload{Reference: reference, Force: force}, details: map[string]any{"reference": reference, "force": force}})
}

func (h *handlers) handleRemoveVolume(w http.ResponseWriter, r *http.Request, session auth.Session) {
	name := r.PathValue("name")
	h.doDocker(w, r, session, dockerAction{action: protocol.ActionDockerVolumeRemove, payload: protocol.NamePayload{Name: name},
		details: map[string]any{"volume": name}})
}

func (h *handlers) handleRemoveNetwork(w http.ResponseWriter, r *http.Request, session auth.Session) {
	name := r.PathValue("name")
	h.doDocker(w, r, session, dockerAction{action: protocol.ActionDockerNetworkRemove, payload: protocol.NamePayload{Name: name},
		details: map[string]any{"network": name}})
}

func (h *handlers) handleGetStack(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var result protocol.StackFilesPayload
	h.getDocker(w, r, session, protocol.ActionDockerStackGet, protocol.StackPayload{Name: r.PathValue("name")}, &result)
}

// handleSaveStack validates, or saves and audits, a stack's files. The
// audit entry names the stack, never the files' contents.
func (h *handlers) handleSaveStack(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request SaveStackRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	name := r.PathValue("name")
	payload := protocol.StackSavePayload{Name: name, Compose: request.Compose, Env: request.Env, ValidateOnly: request.ValidateOnly}
	if request.ValidateOnly {
		agent, ok := h.onlineMachine(w, r, session)
		if !ok {
			return
		}
		if err := h.ask(r.Context(), agent, protocol.ActionDockerStackSave, payload, nil); err != nil {
			h.writeAgentError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.doDocker(w, r, session, dockerAction{action: protocol.ActionDockerStackSave, payload: payload,
		details: map[string]any{"stack": name}})
}

// stackActions maps the path's action to the agent's, with its time limit.
func stackActions() map[string]dockerAction {
	return map[string]dockerAction{
		"up":   {action: protocol.ActionDockerStackUp, timeout: deployTimeout},
		"down": {action: protocol.ActionDockerStackDown, timeout: deployTimeout},
		"pull": {action: protocol.ActionDockerStackPull, timeout: pullTimeout},
	}
}

func (h *handlers) handleStackAction(w http.ResponseWriter, r *http.Request, session auth.Session) {
	request, known := stackActions()[r.PathValue("action")]
	if !known {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "There is no API endpoint at this address.")
		return
	}
	name := r.PathValue("name")
	request.payload, request.details = protocol.StackPayload{Name: name}, map[string]any{"stack": name}
	h.doDocker(w, r, session, request)
}

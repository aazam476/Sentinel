// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"strconv"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/scripts"
)

// batchesShown is how many recent batches the runs list shows.
const batchesShown = 100

// Script is a script in the library.
type Script struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Language       string `json:"language"`
	Body           string `json:"body"`
	TargetOS       string `json:"targetOS"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
}

// Scripts lists the library.
type Scripts struct {
	Scripts []Script `json:"scripts"`
}

// SaveScriptRequest adds or changes a script.
type SaveScriptRequest struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Language       string `json:"language"`
	Body           string `json:"body"`
	TargetOS       string `json:"targetOS"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// ScriptTarget is where a script runs: kind "agent" (value is a machine's
// ID), "tag" (a tag) or "agents" (machine IDs, comma-separated).
type ScriptTarget struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// RunScriptRequest runs a script.
type RunScriptRequest struct {
	Target ScriptTarget `json:"target"`
}

// RunScriptResponse names the batch the run started.
type RunScriptResponse struct {
	BatchID int64 `json:"batchId"`
}

// ScriptBatch is one script run on some machines.
type ScriptBatch struct {
	ID         int64  `json:"id"`
	ScriptID   *int64 `json:"scriptId" tstype:"number | null,required"`
	ScriptName string `json:"scriptName"`
	// StartedBy is the person who started it; empty when a schedule did.
	StartedBy  string `json:"startedBy"`
	ScheduleID *int64 `json:"scheduleId" tstype:"number | null,required"`
	Target     string `json:"target"`
	StartedAt  int64  `json:"startedAt"`
	// Counts says how many runs are in each state.
	Counts map[string]int `json:"counts"`
}

// ScriptBatches lists recent batches, newest first.
type ScriptBatches struct {
	Batches []ScriptBatch `json:"batches"`
}

// ScriptBatchDetails is a batch with the script as it ran and each run.
type ScriptBatchDetails struct {
	ScriptBatch `tstype:",extends"`
	Language    string      `json:"language"`
	Body        string      `json:"body"`
	Runs        []ScriptRun `json:"runs"`
}

// ScriptRun is one machine's run.
type ScriptRun struct {
	ID        int64  `json:"id"`
	BatchID   int64  `json:"batchId"`
	AgentID   string `json:"agentId"`
	AgentName string `json:"agentName"`
	// State is running, succeeded, failed, timed_out, offline, skipped or
	// canceled.
	State      string `json:"state"`
	ExitCode   *int   `json:"exitCode" tstype:"number | null,required"`
	Error      string `json:"error"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt *int64 `json:"finishedAt" tstype:"number | null,required"`
}

// ScriptRunOutput is a run with its output so far.
type ScriptRunOutput struct {
	ScriptRun `tstype:",extends"`
	Output    string `json:"output"`
	// Truncated means only the first 1 MB was kept.
	Truncated bool `json:"truncated"`
}

// Schedule runs a script on a target at the times its cron gives.
type Schedule struct {
	ID         int64        `json:"id"`
	ScriptID   int64        `json:"scriptId"`
	ScriptName string       `json:"scriptName"`
	Cron       string       `json:"cron"`
	Timezone   string       `json:"timezone"`
	Target     ScriptTarget `json:"target"`
	Enabled    bool         `json:"enabled"`
	NextRunAt  *int64       `json:"nextRunAt" tstype:"number | null,required"`
	LastRunAt  *int64       `json:"lastRunAt" tstype:"number | null,required"`
}

// Schedules lists every schedule.
type Schedules struct {
	Schedules []Schedule `json:"schedules"`
}

// SaveScheduleRequest adds or changes a schedule.
type SaveScheduleRequest struct {
	ScriptID int64        `json:"scriptId"`
	Cron     string       `json:"cron"`
	Timezone string       `json:"timezone"`
	Target   ScriptTarget `json:"target"`
	Enabled  bool         `json:"enabled"`
}

// addScriptRoutes adds the script library, runs and schedules (§9.8),
// for those who may run scripts.
func (h *handlers) addScriptRoutes(mux *http.ServeMux) {
	run := func(handler func(http.ResponseWriter, *http.Request, auth.Session)) http.HandlerFunc {
		return h.withPermission(auth.PermissionRunScripts, handler)
	}
	mux.HandleFunc("GET /api/scripts", run(h.handleListScripts))
	mux.HandleFunc("POST /api/scripts", run(h.handleSaveScript))
	mux.HandleFunc("PUT /api/scripts/{script}", run(h.handleSaveScript))
	mux.HandleFunc("DELETE /api/scripts/{script}", run(h.handleDeleteScript))
	mux.HandleFunc("POST /api/scripts/{script}/run", run(h.handleRunScript))
	mux.HandleFunc("GET /api/script-batches", run(h.handleListBatches))
	mux.HandleFunc("GET /api/script-batches/{batch}", run(h.handleGetBatch))
	mux.HandleFunc("GET /api/script-runs/{run}", run(h.handleRunOutput))
	mux.HandleFunc("POST /api/script-runs/{run}/cancel", run(h.handleCancelRun))
	mux.HandleFunc("GET /api/schedules", run(h.handleListSchedules))
	mux.HandleFunc("POST /api/schedules", run(h.handleSaveSchedule))
	mux.HandleFunc("PUT /api/schedules/{schedule}", run(h.handleSaveSchedule))
	mux.HandleFunc("DELETE /api/schedules/{schedule}", run(h.handleDeleteSchedule))
}

// namedID reads the numeric ID in the named path segment; 0 when the path
// has none, as on a POST that adds something.
func namedID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	text := r.PathValue(name)
	if text == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "There is nothing with that ID.")
		return 0, false
	}
	return id, true
}

func (h *handlers) handleListScripts(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	list, err := h.scripts.Scripts(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := Scripts{Scripts: make([]Script, 0, len(list))}
	for _, script := range list {
		response.Scripts = append(response.Scripts, Script(script))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleSaveScript(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "script")
	if !ok {
		return
	}
	var request SaveScriptRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	saved, err := h.scripts.SaveScript(r.Context(), session, requestInfo(r), scripts.Script{
		ID: id, Name: request.Name, Description: request.Description, Language: request.Language, Body: request.Body,
		TargetOS: request.TargetOS, TimeoutSeconds: request.TimeoutSeconds,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Script(saved))
}

func (h *handlers) handleDeleteScript(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "script")
	if !ok {
		return
	}
	if err := h.scripts.DeleteScript(r.Context(), session, requestInfo(r), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) handleRunScript(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "script")
	if !ok {
		return
	}
	var request RunScriptRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	batchID, err := h.scripts.Start(r.Context(), session, requestInfo(r), id, scripts.Target(request.Target))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, RunScriptResponse{BatchID: batchID})
}

func batchView(batch scripts.Batch) ScriptBatch {
	return ScriptBatch{
		ID: batch.ID, ScriptID: batch.ScriptID, ScriptName: batch.ScriptName, StartedBy: batch.StartedBy,
		ScheduleID: batch.ScheduleID, Target: batch.Target, StartedAt: batch.StartedAt, Counts: batch.Counts,
	}
}

func runView(run scripts.Run) ScriptRun {
	return ScriptRun{
		ID: run.ID, BatchID: run.BatchID, AgentID: run.AgentPublic, AgentName: run.AgentName, State: run.State,
		ExitCode: run.ExitCode, Error: run.Error, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}

func (h *handlers) handleListBatches(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	batches, err := h.scripts.Batches(r.Context(), batchesShown)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := ScriptBatches{Batches: make([]ScriptBatch, 0, len(batches))}
	for _, batch := range batches {
		response.Batches = append(response.Batches, batchView(batch))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleGetBatch(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	id, ok := namedID(w, r, "batch")
	if !ok {
		return
	}
	batch, err := h.scripts.GetBatch(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := ScriptBatchDetails{ScriptBatch: batchView(batch), Language: batch.Language, Body: batch.Body, Runs: []ScriptRun{}}
	for _, run := range batch.Runs {
		response.Runs = append(response.Runs, runView(run))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleRunOutput(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	id, ok := namedID(w, r, "run")
	if !ok {
		return
	}
	run, err := h.scripts.RunOutput(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ScriptRunOutput{ScriptRun: runView(run), Output: run.Output, Truncated: run.Truncated})
}

func (h *handlers) handleCancelRun(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "run")
	if !ok {
		return
	}
	if err := h.scripts.Cancel(session, id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func scheduleView(schedule scripts.Schedule) Schedule {
	return Schedule{
		ID: schedule.ID, ScriptID: schedule.ScriptID, ScriptName: schedule.ScriptName, Cron: schedule.Cron,
		Timezone: schedule.Timezone, Target: ScriptTarget(schedule.Target), Enabled: schedule.Enabled,
		NextRunAt: schedule.NextRunAt, LastRunAt: schedule.LastRunAt,
	}
}

func (h *handlers) handleListSchedules(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	list, err := h.scripts.Schedules(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := Schedules{Schedules: make([]Schedule, 0, len(list))}
	for _, schedule := range list {
		response.Schedules = append(response.Schedules, scheduleView(schedule))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleSaveSchedule(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "schedule")
	if !ok {
		return
	}
	var request SaveScheduleRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	saved, err := h.scripts.SaveSchedule(r.Context(), session, requestInfo(r), scripts.Schedule{
		ID: id, ScriptID: request.ScriptID, Cron: request.Cron, Timezone: request.Timezone,
		Target: scripts.Target(request.Target), Enabled: request.Enabled,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduleView(saved))
}

func (h *handlers) handleDeleteSchedule(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id, ok := namedID(w, r, "schedule")
	if !ok {
		return
	}
	if err := h.scripts.DeleteSchedule(r.Context(), session, requestInfo(r), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

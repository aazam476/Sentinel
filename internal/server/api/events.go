// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// AgentEvent is sent on /api/events as "event: agent" when a machine comes
// online, goes offline or reports new details. The UI refetches what it
// shows about that machine.
type AgentEvent struct {
	AgentID string `json:"agentId"`
	// Kind is "online", "offline" or "info_updated".
	Kind string `json:"kind"`
}

// MetricsEvent is sent on /api/events as "event: metrics" for every new
// sample, so live charts can move without polling.
type MetricsEvent struct {
	AgentID string         `json:"agentId"`
	Current CurrentMetrics `json:"current"`
}

// eventStreamCheckInterval is how often an open event stream re-checks the
// session (so a revoked session stops receiving) and sends a comment line
// (so proxies don't close a quiet connection).
const eventStreamCheckInterval = 30 * time.Second

// handleEvents streams fleet changes to the browser as server-sent events.
// It ends when the browser disconnects, the session ends or the server
// shuts down; the browser's EventSource reconnects on its own.
func (h *handlers) handleEvents(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		h.writeError(w, auth.ErrNotAuthenticated)
		return
	}
	events, stop := h.hub.Subscribe()
	defer stop()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// nginx buffers responses unless told not to, which would hold events back.
	w.Header().Set("X-Accel-Buffering", "no")
	stream := &eventStream{w: w, controller: http.NewResponseController(w)}
	if !stream.comment("connected") {
		return
	}

	check := h.clock.NewTimer(eventStreamCheckInterval)
	defer check.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open || !h.sendEvent(stream, event) {
				return
			}
		case <-check.C():
			if _, err := h.auth.CheckSession(r.Context(), cookie.Value); err != nil || !stream.comment("") {
				return
			}
			check.Reset(eventStreamCheckInterval)
		}
	}
}

func (h *handlers) sendEvent(stream *eventStream, event hub.Event) bool {
	if event.Kind != hub.EventMetrics {
		return stream.send("agent", AgentEvent{AgentID: event.PublicID, Kind: string(event.Kind)})
	}
	snapshot, ok := h.metrics.Latest(event.AgentID)
	if !ok {
		return true // revoked in the meantime; nothing to send
	}
	return stream.send("metrics", MetricsEvent{AgentID: event.PublicID, Current: *currentMetrics(snapshot)})
}

// eventStream writes the server-sent events format.
type eventStream struct {
	w          http.ResponseWriter
	controller *http.ResponseController
}

func (s *eventStream) send(name string, data any) bool {
	encoded, err := json.Marshal(data)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, encoded); err != nil {
		return false
	}
	return s.controller.Flush() == nil
}

// comment sends a line the browser ignores; it keeps the connection busy.
func (s *eventStream) comment(text string) bool {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return false
	}
	return s.controller.Flush() == nil
}

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/relay"
)

// errorCodeDesktopUnavailable: the machine has no desktop to share now.
const errorCodeDesktopUnavailable = "desktop_unavailable"

// handleDesktop is GET /api/agents/{id}/desktop: a WebSocket carrying a
// remote desktop session's signaling and, when WebRTC can't connect, its
// screen (§9.3). Like the terminal, a plain GET runs the checks only.
func (h *handlers) handleDesktop(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.checkDesktop(w, r, session)
	if !ok {
		return
	}
	if !isWebSocketUpgrade(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// The Origin was checked by middleware.RequireSameOrigin (SEC-13); see
	// handleShell.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return // Accept has already answered
	}
	defer conn.CloseNow() //nolint:errcheck // the session is over either way
	h.runDesktop(r.Context(), conn, session, agent, r.URL.Query().Get("viewOnly") == "true", requestInfo(r))
}

// checkDesktop applies the step-up (SEC-07), local policy, online and
// availability checks.
func (h *handlers) checkDesktop(w http.ResponseWriter, r *http.Request, session auth.Session) (fleet.Agent, bool) {
	if err := h.auth.RequireRecentSecondFactor(session); err != nil {
		h.writeError(w, err)
		return fleet.Agent{}, false
	}
	agent, err := h.fleet.GetAgent(r.Context(), session, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return fleet.Agent{}, false
	}
	if !allowsCapability(agent, "desktop") {
		writeError(w, http.StatusForbidden, errorCodePolicyDenied, "Remote desktop is disabled by this machine’s local policy.")
		return fleet.Agent{}, false
	}
	if !h.hub.IsOnline(agent.ID) {
		writeError(w, http.StatusConflict, errorCodeMachineOffline, "The machine is offline.")
		return fleet.Agent{}, false
	}
	if available, reason := desktopAvailability(agent); !available {
		writeError(w, http.StatusConflict, errorCodeDesktopUnavailable, reason)
		return fleet.Agent{}, false
	}
	return agent, true
}

// desktopAvailability is what the agent last said about its desktop.
func desktopAvailability(agent fleet.Agent) (bool, string) {
	desktop := agent.SystemInfo.Desktop
	if desktop == nil {
		return false, "This machine’s agent is too old for remote desktop; update it."
	}
	return desktop.Available, desktop.Reason
}

// runDesktop starts the session on the agent and relays it, auditing the
// start and the end (SEC-40).
func (h *handlers) runDesktop(ctx context.Context, conn *websocket.Conn, session auth.Session, agent fleet.Agent, viewOnly bool, client auth.RequestInfo) {
	var started protocol.DesktopStartedPayload
	stream, err := h.hub.OpenStream(ctx, agent.ID, protocol.ActionDesktopSessionStart, func(id protocol.StreamID) any {
		return protocol.DesktopStartPayload{StreamID: id, ViewOnly: viewOnly}
	}, &started)
	if err != nil {
		h.auditMachine(ctx, session, agent, client, "desktop.open", outcomeOf(err), map[string]any{"error": err.Error()})
		_ = conn.Close(websocket.StatusPolicyViolation, closeReason(err)) // best effort; the browser shows the reason
		return
	}
	h.auditMachine(ctx, session, agent, client, "desktop.open", audit.OutcomeStarted, map[string]any{
		"source": started.Source, "viewOnly": viewOnly,
	})
	ready, _ := encodeDesktopReady(started) // a struct of strings and numbers always encodes
	if err := conn.Write(ctx, websocket.MessageText, ready); err != nil {
		stream.Close(context.WithoutCancel(ctx), "the viewer left")
		return
	}
	startedAt := h.clock.Now()
	result := relay.PipeDesktop(ctx, conn, stream)
	h.auditMachine(context.WithoutCancel(ctx), session, agent, client, "desktop.close", audit.OutcomeSuccess, map[string]any{
		"reason": result.Reason, "durationSeconds": int64(h.clock.Now().Sub(startedAt) / time.Second),
		"bytesIn": result.BytesIn, "bytesOut": result.BytesOut,
	})
	_ = conn.Close(websocket.StatusNormalClosure, "") // best effort; the browser may be gone
}

// DesktopReady is the first message on a desktop WebSocket: the session
// started on the agent.
type DesktopReady struct {
	// Type is "ready".
	Type   string `json:"type"`
	Source string `json:"source"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func encodeDesktopReady(started protocol.DesktopStartedPayload) ([]byte, error) {
	return json.Marshal(DesktopReady{Type: "ready", Source: started.Source, Width: started.Width, Height: started.Height})
}

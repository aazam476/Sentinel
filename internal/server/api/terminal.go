// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/relay"
)

// Error codes for actions on a machine.
const (
	errorCodeMachineOffline = "machine_offline"
	errorCodePolicyDenied   = "policy_denied"
)

// terminalRequest is what the browser asks for in the query string.
type terminalRequest struct {
	cols, rows uint16
	resume     string
}

// handleTerminal is GET /api/agents/{id}/terminal: a WebSocket to a shell on
// the machine (§9.2). Everything that can be refused is checked before the
// upgrade, so the browser gets a normal error response it can show.
func (h *handlers) handleTerminal(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, request, ok := h.checkTerminal(w, r, session)
	if !ok {
		return
	}
	// The Origin header was already checked against the server's own
	// origin (middleware.RequireSameOrigin, SEC-13). The library's own check
	// compares it with the Host header instead, which a reverse proxy may
	// rewrite, so it is skipped.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return // Accept has already answered
	}
	defer conn.CloseNow() //nolint:errcheck // the session is over either way
	h.runTerminal(r.Context(), conn, session, agent, request, requestInfo(r))
}

// checkTerminal applies the permission, step-up (SEC-07), online and local
// policy checks, writing the error response when one fails.
func (h *handlers) checkTerminal(w http.ResponseWriter, r *http.Request, session auth.Session) (fleet.Agent, terminalRequest, bool) {
	if err := h.auth.RequireRecentSecondFactor(session); err != nil {
		h.writeError(w, err)
		return fleet.Agent{}, terminalRequest{}, false
	}
	agent, err := h.fleet.GetAgent(r.Context(), session, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return fleet.Agent{}, terminalRequest{}, false
	}
	if !allowsCapability(agent, "terminal") {
		writeError(w, http.StatusForbidden, errorCodePolicyDenied, "The terminal is disabled by this machine’s local policy.")
		return fleet.Agent{}, terminalRequest{}, false
	}
	if !h.hub.IsOnline(agent.ID) {
		writeError(w, http.StatusConflict, errorCodeMachineOffline, "The machine is offline.")
		return fleet.Agent{}, terminalRequest{}, false
	}
	query := r.URL.Query()
	request := terminalRequest{cols: terminalSize(query.Get("cols"), 80), rows: terminalSize(query.Get("rows"), 24), resume: query.Get("resume")}
	if request.resume != "" && !h.terminals.CanResume(request.resume, session.UserID, agent.ID) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "That terminal session has ended.")
		return fleet.Agent{}, terminalRequest{}, false
	}
	return agent, request, true
}

// runTerminal opens the shell on the agent and relays it until it ends,
// auditing the start and the end (SEC-40).
func (h *handlers) runTerminal(ctx context.Context, conn *websocket.Conn, session auth.Session, agent fleet.Agent, request terminalRequest, client auth.RequestInfo) {
	var opened protocol.TerminalOpenedPayload
	stream, err := h.hub.OpenStream(ctx, agent.ID, protocol.ActionTerminalOpen, func(id protocol.StreamID) any {
		return protocol.TerminalOpenPayload{StreamID: id, Cols: request.cols, Rows: request.rows, ResumeSessionID: request.resume}
	}, &opened)
	if err != nil {
		h.auditTerminal(ctx, session, agent, client, "terminal.open", outcomeOf(err), map[string]any{"error": err.Error()})
		_ = conn.Close(websocket.StatusPolicyViolation, closeReason(err)) // best effort; the browser shows the reason
		return
	}
	h.terminals.Attached(opened.SessionID, session.UserID, agent.ID)
	h.auditTerminal(ctx, session, agent, client, "terminal.open", audit.OutcomeStarted,
		map[string]any{"sessionId": opened.SessionID, "resumed": opened.Resumed})

	startedAt := h.clock.Now()
	resize := func(ctx context.Context, cols, rows uint16) error {
		return h.hub.Request(ctx, agent.ID, protocol.ActionTerminalResize,
			protocol.TerminalResizePayload{StreamID: stream.ID, Cols: cols, Rows: rows}, nil)
	}
	result := relay.Pipe(ctx, conn, stream, relay.Status{SessionID: opened.SessionID, Resumed: opened.Resumed}, resize)
	if result.Detached {
		h.terminals.Detached(opened.SessionID)
	} else {
		h.terminals.Ended(opened.SessionID)
	}
	h.auditTerminal(context.WithoutCancel(ctx), session, agent, client, "terminal.close", audit.OutcomeSuccess, map[string]any{
		"sessionId": opened.SessionID, "reason": result.Reason, "detached": result.Detached,
		"durationSeconds": int64(h.clock.Now().Sub(startedAt) / time.Second),
		"bytesIn":         result.BytesIn, "bytesOut": result.BytesOut,
	})
	// Closed only now, so whoever sees the connection end also finds the
	// session's end in the audit log.
	_ = conn.Close(websocket.StatusNormalClosure, "") // best effort; the browser may be gone
}

func (h *handlers) auditTerminal(ctx context.Context, session auth.Session, agent fleet.Agent, client auth.RequestInfo, action string, outcome audit.Outcome, details map[string]any) {
	userID, agentID := session.UserID, agent.ID
	details["agentId"], details["machine"] = agent.PublicID, agent.Name()
	entry := audit.Entry{UserID: &userID, Username: session.Username, AgentID: &agentID, Action: action, Outcome: outcome, IP: client.IP, Details: details}
	if err := h.auditLog.Record(ctx, entry); err != nil {
		h.logger.Error("could not audit a terminal session", "action", action, "error", err)
	}
}

// outcomeOf audits a refusal by the agent's policy as denied, anything
// else as a failure.
func outcomeOf(err error) audit.Outcome {
	var agentErr *protocol.Error
	if errors.As(err, &agentErr) && agentErr.Code == protocol.ErrorPolicyDenied {
		return audit.OutcomeDenied
	}
	return audit.OutcomeFailure
}

// closeReason is the WebSocket close reason the browser shows; WebSocket
// limits it to 123 bytes.
func closeReason(err error) string {
	var agentErr *protocol.Error
	reason := "The terminal couldn’t be opened."
	if errors.As(err, &agentErr) {
		reason = agentErr.Message
	}
	if len(reason) > 120 {
		reason = reason[:120]
	}
	return reason
}

// allowsCapability reports whether the machine's local policy allows a
// capability. A machine that hasn't reported yet is given the benefit of
// the doubt: the agent checks its policy itself either way.
func allowsCapability(agent fleet.Agent, name string) bool {
	for _, capability := range agent.SystemInfo.Capabilities {
		if capability.Name == name {
			return capability.Enabled
		}
	}
	return true
}

// terminalSize reads a column or row count, keeping it between 1 and 1,000.
func terminalSize(value string, fallback uint16) uint16 {
	size, err := strconv.ParseUint(value, 10, 16)
	if err != nil || size == 0 {
		return fallback
	}
	return uint16(min(size, 1000))
}

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"strconv"
	"strings"
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
	// user is who opens it, shown in the machine's tray.
	user string
}

// shellTarget is what a terminal WebSocket opens: the machine's own shell,
// or a shell in one of its containers. Everything else about the two is
// the same: the checks, step-up, relaying, resuming and auditing.
type shellTarget struct {
	// capability is the local policy switch it needs, and what names it
	// in the refusal.
	capability, what string
	// action and payload open it on the agent.
	action  string
	payload func(stream protocol.StreamID, request terminalRequest) any
	// auditPrefix names its audit entries (prefix.open, prefix.close);
	// details go in each.
	auditPrefix string
	details     map[string]any
}

// machineShell is the machine's own shell (§9.2).
func machineShell() shellTarget {
	return shellTarget{capability: "terminal", what: "The terminal", action: protocol.ActionTerminalOpen,
		payload: func(stream protocol.StreamID, request terminalRequest) any {
			return protocol.TerminalOpenPayload{StreamID: stream, Cols: request.cols, Rows: request.rows, ResumeSessionID: request.resume, User: request.user}
		},
		auditPrefix: "terminal", details: map[string]any{}}
}

// containerShell is a shell in a container (§9.4).
func containerShell(container string) shellTarget {
	return shellTarget{capability: "docker", what: "Docker", action: protocol.ActionDockerContainerExec,
		payload: func(stream protocol.StreamID, request terminalRequest) any {
			return protocol.ContainerExecPayload{ID: container, StreamID: stream, Cols: request.cols, Rows: request.rows,
				ResumeSessionID: request.resume, User: request.user}
		},
		auditPrefix: "docker.exec", details: map[string]any{"container": container}}
}

// handleTerminal is GET /api/agents/{id}/terminal: a WebSocket to a shell on
// the machine (§9.2). Everything that can be refused is checked before the
// upgrade, so the browser gets a normal error response it can show.
func (h *handlers) handleTerminal(w http.ResponseWriter, r *http.Request, session auth.Session) {
	h.handleShell(w, r, session, machineShell())
}

// handleContainerExec is GET /api/agents/{id}/docker/containers/{container}/exec:
// the same as the terminal, with a shell in the container.
func (h *handlers) handleContainerExec(w http.ResponseWriter, r *http.Request, session auth.Session) {
	container := r.PathValue("container")
	if container == "" || len(container) > 255 {
		writeInvalidInput(w, "container", "That isn’t a container name.")
		return
	}
	h.handleShell(w, r, session, containerShell(container))
}

func (h *handlers) handleShell(w http.ResponseWriter, r *http.Request, session auth.Session, target shellTarget) {
	agent, request, ok := h.checkTerminal(w, r, session, target)
	if !ok {
		return
	}
	// Browsers don't show a page why a WebSocket upgrade was refused, so
	// the UI first sends a plain GET: it runs the same checks and gets
	// either their error (step-up needed, offline, ...) or 204.
	if !isWebSocketUpgrade(r) {
		w.WriteHeader(http.StatusNoContent)
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
	h.runTerminal(r.Context(), conn, session, agent, request, requestInfo(r), target)
}

// checkTerminal applies the permission, step-up (SEC-07), online and local
// policy checks, writing the error response when one fails.
func (h *handlers) checkTerminal(w http.ResponseWriter, r *http.Request, session auth.Session, target shellTarget) (fleet.Agent, terminalRequest, bool) {
	if err := h.auth.RequireRecentSecondFactor(session); err != nil {
		h.writeError(w, err)
		return fleet.Agent{}, terminalRequest{}, false
	}
	agent, err := h.fleet.GetAgent(r.Context(), session, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return fleet.Agent{}, terminalRequest{}, false
	}
	if !allowsCapability(agent, target.capability) {
		writeError(w, http.StatusForbidden, errorCodePolicyDenied, target.what+" is disabled by this machine’s local policy.")
		return fleet.Agent{}, terminalRequest{}, false
	}
	if !h.hub.IsOnline(agent.ID) {
		writeError(w, http.StatusConflict, errorCodeMachineOffline, "The machine is offline.")
		return fleet.Agent{}, terminalRequest{}, false
	}
	query := r.URL.Query()
	request := terminalRequest{cols: terminalSize(query.Get("cols"), 80), rows: terminalSize(query.Get("rows"), 24), resume: query.Get("resume"), user: session.Username}
	if request.resume != "" && !h.terminals.CanResume(request.resume, session.UserID, agent.ID) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "That terminal session has ended.")
		return fleet.Agent{}, terminalRequest{}, false
	}
	return agent, request, true
}

// runTerminal opens the shell on the agent and relays it until it ends,
// auditing the start and the end (SEC-40).
func (h *handlers) runTerminal(ctx context.Context, conn *websocket.Conn, session auth.Session, agent fleet.Agent, request terminalRequest, client auth.RequestInfo, target shellTarget) {
	details := func(more map[string]any) map[string]any {
		maps.Copy(more, target.details)
		return more
	}
	var opened protocol.TerminalOpenedPayload
	stream, err := h.hub.OpenStream(ctx, agent.ID, target.action, func(id protocol.StreamID) any {
		return target.payload(id, request)
	}, &opened)
	if err != nil {
		h.auditMachine(ctx, session, agent, client, target.auditPrefix+".open", outcomeOf(err), details(map[string]any{"error": err.Error()}))
		_ = conn.Close(websocket.StatusPolicyViolation, closeReason(err)) // best effort; the browser shows the reason
		return
	}
	h.terminals.Attached(opened.SessionID, session.UserID, agent.ID)
	h.auditMachine(ctx, session, agent, client, target.auditPrefix+".open", audit.OutcomeStarted,
		details(map[string]any{"sessionId": opened.SessionID, "resumed": opened.Resumed}))

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
	h.auditMachine(context.WithoutCancel(ctx), session, agent, client, target.auditPrefix+".close", audit.OutcomeSuccess, details(map[string]any{
		"sessionId": opened.SessionID, "reason": result.Reason, "detached": result.Detached,
		"durationSeconds": int64(h.clock.Now().Sub(startedAt) / time.Second),
		"bytesIn":         result.BytesIn, "bytesOut": result.BytesOut,
	}))
	// Closed only now, so whoever sees the connection end also finds the
	// session's end in the audit log.
	_ = conn.Close(websocket.StatusNormalClosure, "") // best effort; the browser may be gone
}

// auditMachine records an action on a machine (SEC-40), naming the
// machine in the details.
func (h *handlers) auditMachine(ctx context.Context, session auth.Session, agent fleet.Agent, client auth.RequestInfo, action string, outcome audit.Outcome, details map[string]any) {
	userID, agentID := session.UserID, agent.ID
	details["agentId"], details["machine"] = agent.PublicID, agent.Name()
	entry := audit.Entry{UserID: &userID, Username: session.Username, AgentID: &agentID, Action: action, Outcome: outcome, IP: client.IP, Details: details}
	if err := h.auditLog.Record(ctx, entry); err != nil {
		h.logger.Error("could not audit an action on a machine", "action", action, "error", err)
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

// isWebSocketUpgrade reports whether r asks to become a WebSocket.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// terminalSize reads a column or row count, keeping it between 1 and 1,000.
func terminalSize(value string, fallback uint16) uint16 {
	size, err := strconv.ParseUint(value, 10, 16)
	if err != nil || size == 0 {
		return fallback
	}
	return uint16(min(size, 1000))
}

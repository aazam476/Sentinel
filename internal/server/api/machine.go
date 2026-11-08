// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// Error codes for requests the agent answers.
const (
	errorCodeMachineTimeout = "machine_timeout"
	errorCodeUnsupported    = "unsupported"
	errorCodeAgentFailed    = "agent_failed"
)

// onlineMachine finds the machine in the path and checks it is online,
// writing the error response when not.
func (h *handlers) onlineMachine(w http.ResponseWriter, r *http.Request, session auth.Session) (fleet.Agent, bool) {
	agent, err := h.fleet.GetAgent(r.Context(), session, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return fleet.Agent{}, false
	}
	if !h.hub.IsOnline(agent.ID) {
		writeError(w, http.StatusConflict, errorCodeMachineOffline, "The machine is offline.")
		return fleet.Agent{}, false
	}
	return agent, true
}

// ask sends an action to the machine's agent and decodes its answer.
func (h *handlers) ask(ctx context.Context, agent fleet.Agent, action string, payload, result any) error {
	return h.hub.Request(ctx, agent.ID, action, payload, result)
}

// writeAgentError reports why a request to an agent failed. The agent's
// own messages are shown as they are: they explain the machine's side.
func (h *handlers) writeAgentError(w http.ResponseWriter, err error) {
	var agentErr *protocol.Error
	switch {
	case errors.Is(err, hub.ErrOffline), errors.Is(err, hub.ErrDisconnected):
		writeError(w, http.StatusConflict, errorCodeMachineOffline, "The machine went offline before it answered.")
	case errors.Is(err, hub.ErrTimeout):
		writeError(w, http.StatusGatewayTimeout, errorCodeMachineTimeout, "The machine didn’t answer in time.")
	case errors.As(err, &agentErr):
		status, code := agentErrorStatus(agentErr.Code)
		writeError(w, status, code, agentErr.Message)
	default:
		h.logger.Error("a request to an agent failed", "error", err)
		writeError(w, http.StatusInternalServerError, errorCodeInternal, "Something went wrong on the server. The details are in its log.")
	}
}

func agentErrorStatus(code protocol.ErrorCode) (int, string) {
	switch code {
	case protocol.ErrorPolicyDenied:
		return http.StatusForbidden, errorCodePolicyDenied
	case protocol.ErrorNotFound:
		return http.StatusNotFound, errorCodeNotFound
	case protocol.ErrorInvalidRequest:
		return http.StatusBadRequest, errorCodeInvalidInput
	case protocol.ErrorUnsupportedPlatform, protocol.ErrorUnknownAction:
		return http.StatusNotImplemented, errorCodeUnsupported
	default:
		return http.StatusBadGateway, errorCodeAgentFailed
	}
}

// doAudited sends a state-changing action to the agent and audits it with
// its outcome (SEC-40): success, denied by the local policy, or failure.
func (h *handlers) doAudited(r *http.Request, session auth.Session, agent fleet.Agent, action string, payload any, details map[string]any) error {
	err := h.ask(r.Context(), agent, action, payload, nil)
	outcome := audit.OutcomeSuccess
	if err != nil {
		outcome = outcomeOf(err)
		details["error"] = err.Error()
	}
	h.auditMachine(context.WithoutCancel(r.Context()), session, agent, requestInfo(r), action, outcome, details)
	return err
}

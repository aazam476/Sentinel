// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"maps"

	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/power"
	"github.com/aazam476/sentinel/internal/agent/services"
	"github.com/aazam476/sentinel/internal/agent/terminal"
	"github.com/aazam476/sentinel/internal/protocol"
)

// handler answers one action: the response payload, or an error to send.
type handler func(ctx context.Context, request protocol.Message) (any, *protocol.Error)

// handlers lists every action the agent answers. Each one that does
// something on the machine checks the local policy first (SEC-22).
func (c *connection) handlers() map[string]handler {
	handlers := map[string]handler{
		protocol.ActionSystemInfo:     c.systemInfo,
		protocol.ActionTerminalOpen:   c.openTerminal,
		protocol.ActionTerminalResize: c.resizeTerminal,
		protocol.ActionServiceList:    c.listServices,
		protocol.ActionServiceStart:   c.serviceAction(services.Start),
		protocol.ActionServiceStop:    c.serviceAction(services.Stop),
		protocol.ActionServiceRestart: c.serviceAction(services.Restart),
		protocol.ActionServiceEnable:  c.serviceAction(services.Enable),
		protocol.ActionServiceDisable: c.serviceAction(services.Disable),
		protocol.ActionServiceLogs:    c.serviceLogs,
		protocol.ActionLogQuery:       c.queryLogs,
		protocol.ActionProcessList:    c.listProcesses,
		protocol.ActionProcessKill:    c.killProcess,
		protocol.ActionSystemReboot:   c.power(power.Reboot),
		protocol.ActionSystemShutdown: c.power(power.Shutdown),
	}
	maps.Copy(handlers, c.dockerHandlers())
	return handlers
}

// answer handles one request from the server and sends the response.
func (c *connection) answer(ctx context.Context, request protocol.Message) {
	response := protocol.Message{Type: protocol.TypeResponse, ID: request.ID}
	var payload any
	if handle, known := c.handlers()[request.Action]; known {
		payload, response.Error = handle(ctx, request)
	} else {
		response.Error = &protocol.Error{
			Code:    protocol.ErrorUnknownAction,
			Message: "This agent doesn't know the action " + request.Action + "; it may need updating.",
		}
	}
	if response.Error != nil {
		payload = nil
	}
	if err := c.send(ctx, response, payload); err != nil {
		c.agent.options.Logger.Debug("could not answer a request", "action", request.Action, "error", err)
	}
}

func (c *connection) systemInfo(ctx context.Context, _ protocol.Message) (any, *protocol.Error) {
	options := c.agent.options
	info, err := options.Collector.SystemInfo(ctx, options.Policy.Report(), options.Version)
	if err != nil {
		options.Logger.Warn("could not describe this machine", "error", err)
		return nil, &protocol.Error{Code: protocol.ErrorInternal, Message: "The agent couldn't read this machine's details."}
	}
	return info, nil
}

func (c *connection) openTerminal(_ context.Context, request protocol.Message) (any, *protocol.Error) {
	if refusal := c.refuseUnlessAllowed(policy.Terminal, "The terminal"); refusal != nil {
		return nil, refusal
	}
	var payload protocol.TerminalOpenPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	opened, err := c.agent.terminals.Open(payload, streamOutput{conn: c})
	switch {
	case errors.Is(err, terminal.ErrSessionEnded):
		return nil, &protocol.Error{Code: protocol.ErrorNotFound, Message: "That terminal session has ended."}
	case err != nil:
		c.agent.options.Logger.Warn("could not open a terminal", "error", err)
		return nil, &protocol.Error{Code: protocol.ErrorInternal, Message: "The agent couldn't start a shell: " + err.Error()}
	}
	return opened, nil
}

func (c *connection) resizeTerminal(_ context.Context, request protocol.Message) (any, *protocol.Error) {
	var payload protocol.TerminalResizePayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	if err := c.agent.terminals.Resize(payload); err != nil {
		return nil, &protocol.Error{Code: protocol.ErrorNotFound, Message: "That terminal session has ended."}
	}
	return nil, nil
}

// refuseUnlessAllowed returns the policy_denied error for a capability the
// local policy switches off. what names it in the message.
func (c *connection) refuseUnlessAllowed(capability, what string) *protocol.Error {
	if c.agent.options.Policy.Allows(capability) {
		return nil
	}
	return &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: what + " is disabled by this machine's local policy."}
}

func invalidRequest(err error) *protocol.Error {
	return &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: err.Error()}
}

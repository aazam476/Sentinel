// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"

	"github.com/aazam476/sentinel/internal/agent/logs"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/power"
	"github.com/aazam476/sentinel/internal/agent/processes"
	"github.com/aazam476/sentinel/internal/agent/services"
	"github.com/aazam476/sentinel/internal/protocol"
)

// The handlers for system management (§9.6). Services, processes and logs
// aren't capabilities the local policy can switch off; power is (SEC-22).

func (c *connection) listServices(ctx context.Context, _ protocol.Message) (any, *protocol.Error) {
	list, err := c.agent.options.Services.List(ctx)
	if err != nil {
		return nil, c.systemError(err, "list the services")
	}
	return list, nil
}

// serviceAction returns the handler for service.start, stop and so on.
func (c *connection) serviceAction(action services.Action) handler {
	return func(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
		var payload protocol.ServiceActionPayload
		if err := protocol.DecodePayload(request, &payload); err != nil {
			return nil, invalidRequest(err)
		}
		if err := c.agent.options.Services.Do(ctx, action, payload.Name); err != nil {
			return nil, c.systemError(err, string(action)+" "+payload.Name)
		}
		return nil, nil
	}
}

func (c *connection) serviceLogs(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	var payload protocol.LogQueryPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	if payload.Service == "" {
		return nil, &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: "service.logs needs a service name."}
	}
	return c.readLogs(ctx, payload)
}

func (c *connection) queryLogs(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	var payload protocol.LogQueryPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	return c.readLogs(ctx, payload)
}

func (c *connection) readLogs(ctx context.Context, query protocol.LogQueryPayload) (any, *protocol.Error) {
	entries, err := c.agent.options.Logs.Query(ctx, query)
	if err != nil {
		return nil, c.systemError(err, "read the system log")
	}
	return entries, nil
}

func (c *connection) listProcesses(ctx context.Context, _ protocol.Message) (any, *protocol.Error) {
	list, err := c.agent.options.Processes.List(ctx)
	if err != nil {
		return nil, c.systemError(err, "list the processes")
	}
	return list, nil
}

func (c *connection) killProcess(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	var payload protocol.ProcessKillPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	if err := c.agent.options.Processes.Kill(ctx, payload); err != nil {
		return nil, c.systemError(err, "end the process")
	}
	return nil, nil
}

// power returns the handler for system.reboot or system.shutdown. It
// answers before the machine goes down; see power.Delay.
func (c *connection) power(action power.Action) handler {
	return func(context.Context, protocol.Message) (any, *protocol.Error) {
		if refusal := c.refuseUnlessAllowed(policy.Power, "Rebooting and shutting down"); refusal != nil {
			return nil, refusal
		}
		done, err := c.agent.options.Power.Schedule(action)
		if err != nil {
			return nil, c.systemError(err, string(action))
		}
		logger := c.agent.options.Logger
		logger.Warn("the machine is going down at the server's request", "action", action)
		go func() {
			if err := <-done; err != nil {
				logger.Error("could not "+string(action)+" the machine", "error", err)
			}
		}()
		return nil, nil
	}
}

// systemError turns an error from a system package into the protocol
// error the server shows. doing names what failed, for the message.
func (c *connection) systemError(err error, doing string) *protocol.Error {
	switch {
	case errors.Is(err, services.ErrInvalidName), errors.Is(err, services.ErrPowerService),
		errors.Is(err, logs.ErrInvalidQuery), errors.Is(err, processes.ErrProtected):
		return &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: err.Error()}
	case errors.Is(err, services.ErrNotFound), errors.Is(err, processes.ErrNotFound):
		return &protocol.Error{Code: protocol.ErrorNotFound, Message: err.Error()}
	case errors.Is(err, services.ErrUnsupported), errors.Is(err, logs.ErrUnsupported), errors.Is(err, power.ErrUnsupported):
		return &protocol.Error{Code: protocol.ErrorUnsupportedPlatform, Message: err.Error()}
	default:
		c.agent.options.Logger.Warn("a system request failed", "doing", doing, "error", err)
		return &protocol.Error{Code: protocol.ErrorInternal, Message: "The agent couldn't " + doing + ": " + err.Error()}
	}
}

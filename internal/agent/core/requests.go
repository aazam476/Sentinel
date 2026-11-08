// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"

	"github.com/aazam476/sentinel/internal/protocol"
)

// answer handles one request from the server and sends the response. Later
// features add their actions here; each checks the local policy first.
func (c *connection) answer(ctx context.Context, request protocol.Message) {
	response := protocol.Message{Type: protocol.TypeResponse, ID: request.ID}
	var payload any
	switch request.Action {
	case protocol.ActionSystemInfo:
		options := c.agent.options
		info, err := options.Collector.SystemInfo(ctx, options.Policy.Report(), options.Version)
		if err != nil {
			options.Logger.Warn("could not describe this machine", "error", err)
			response.Error = &protocol.Error{Code: protocol.ErrorInternal, Message: "The agent couldn't read this machine's details."}
		} else {
			payload = info
		}
	default:
		response.Error = &protocol.Error{
			Code:    protocol.ErrorUnknownAction,
			Message: "This agent doesn't know the action " + request.Action + "; it may need updating.",
		}
	}
	if err := c.send(ctx, response, payload); err != nil {
		c.agent.options.Logger.Debug("could not answer a request", "action", request.Action, "error", err)
	}
}

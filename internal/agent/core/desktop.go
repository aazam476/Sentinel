// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
)

// desktopInputBuffer is how many of the browser's messages wait for a
// session that is busy encoding a frame.
const desktopInputBuffer = 256

// desktopAvailability says whether remote desktop can be used now: the
// local policy allows it and there is a desktop to share.
func (a *Agent) desktopAvailability() protocol.DesktopAvailability {
	if !a.options.Policy.Allows(policy.Desktop) {
		return protocol.DesktopAvailability{Reason: policy.DisabledReason}
	}
	return a.options.Desktop.Availability()
}

// desktopOutput sends a session's messages on its stream.
type desktopOutput struct {
	conn   *connection
	stream protocol.StreamID
}

func (o desktopOutput) Send(ctx context.Context, data []byte) error {
	return streamOutput{conn: o.conn}.SendFrame(ctx, o.stream, data)
}

// startDesktop starts a remote desktop session on the stream (§9.3). It
// runs until the server closes the stream or the connection ends.
func (c *connection) startDesktop(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	if refusal := c.refuseUnlessAllowed(policy.Desktop, "Remote desktop"); refusal != nil {
		return nil, refusal
	}
	var payload protocol.DesktopStartPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	source, err := c.agent.options.Desktop.Open()
	if err != nil {
		if errors.Is(err, desktop.ErrUnavailable) {
			return nil, &protocol.Error{Code: protocol.ErrorUnsupportedPlatform, Message: "Remote desktop isn't available: " + err.Error() + "."}
		}
		return nil, &protocol.Error{Code: protocol.ErrorInternal, Message: "The agent couldn't share the desktop: " + err.Error()}
	}
	inbound := make(chan []byte, desktopInputBuffer)
	c.agent.desktopInputs.Store(payload.StreamID, func(data []byte) {
		select {
		case inbound <- data:
		default: // a browser sending faster than the session reads loses input, not the connection
		}
	})
	session := desktop.NewSession(source, desktop.Options{
		ViewOnly: payload.ViewOnly, ICEServers: payload.ICEServers, Clock: c.agent.options.Clock, Logger: c.agent.options.Logger,
	})
	width, height := source.Size()
	c.follow(ctx, payload.StreamID, func(ctx context.Context) string {
		defer c.agent.desktopInputs.Delete(payload.StreamID)
		defer func() { _ = source.Close() }() // the session is over either way
		if err := session.Run(ctx, inbound, desktopOutput{conn: c, stream: payload.StreamID}); err != nil {
			return err.Error()
		}
		return "the session ended"
	})
	return protocol.DesktopStartedPayload{Source: source.Name(), Width: width, Height: height}, nil
}

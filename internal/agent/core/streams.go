// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"

	"github.com/aazam476/sentinel/internal/protocol"
)

// streamOutput lets a terminal session send on this connection's streams.
type streamOutput struct {
	conn *connection
}

// SendFrame sends a stream's data, giving up after writeTimeout of real
// time (see connection.send).
func (o streamOutput) SendFrame(ctx context.Context, stream protocol.StreamID, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return protocol.WriteFrame(ctx, o.conn.conn, stream, data)
}

// CloseStream tells the server a stream ended, and why.
func (o streamOutput) CloseStream(ctx context.Context, stream protocol.StreamID, reason string) {
	message := protocol.Message{Type: protocol.TypeStreamClose}
	// Best effort: if the connection is gone, the server ended the stream already.
	_ = o.conn.send(ctx, message, protocol.StreamClosePayload{StreamID: stream, Reason: reason})
}

// handleFrame passes the server's stream data (keystrokes) on.
func (c *connection) handleFrame(frame []byte) {
	stream, data, err := protocol.DecodeFrame(frame)
	if err != nil {
		c.agent.options.Logger.Debug("the server sent a malformed stream frame", "error", err)
		return
	}
	if input, desktop := c.agent.desktopInputs.Load(stream); desktop {
		input.(func([]byte))(data) //nolint:forcetypeassert // only startDesktop stores into desktopInputs
		return
	}
	c.agent.terminals.Input(stream, data)
}

// handleStreamClose ends, or detaches, the session behind a stream the
// server closed.
func (c *connection) handleStreamClose(message protocol.Message) {
	var payload protocol.StreamClosePayload
	if err := protocol.DecodePayload(message, &payload); err != nil {
		c.agent.options.Logger.Debug("the server sent a malformed stream.close", "error", err)
		return
	}
	if stop, following := c.followers.LoadAndDelete(payload.StreamID); following {
		stop.(context.CancelFunc)() //nolint:forcetypeassert // only follow stores into followers
		return
	}
	c.agent.terminals.StreamClosed(payload)
}

// follow runs run for a stream until it returns, the server closes the
// stream, or the connection ends. run's reason, if it ends by itself, is
// sent to the server as the stream's close reason.
func (c *connection) follow(connected context.Context, stream protocol.StreamID, run func(ctx context.Context) (reason string)) {
	ctx, stop := context.WithCancel(connected)
	c.followers.Store(stream, stop)
	go func() {
		defer stop()
		reason := run(ctx)
		if _, following := c.followers.LoadAndDelete(stream); following {
			// The connection's context: if it has ended, there's no one to tell.
			streamOutput{conn: c}.CloseStream(connected, stream, reason)
		}
	}()
}

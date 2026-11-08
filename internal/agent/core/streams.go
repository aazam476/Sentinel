// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// streamOutput lets a terminal session send on this connection's streams.
type streamOutput struct {
	conn *connection
}

// SendFrame sends a stream's data, giving up after writeTimeout.
func (o streamOutput) SendFrame(ctx context.Context, stream protocol.StreamID, data []byte) error {
	ctx, cancel := clock.WithTimeout(ctx, o.conn.agent.options.Clock, writeTimeout)
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
	c.agent.terminals.StreamClosed(payload)
}

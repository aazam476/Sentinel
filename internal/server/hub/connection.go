// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// connection is one authenticated agent connection.
type connection struct {
	hub   *Hub
	conn  *websocket.Conn
	agent fleet.Agent
	hello protocol.HelloPayload

	// lastActivity is when the last message arrived, in Unix
	// milliseconds; the heartbeat watchdog reads it.
	lastActivity atomic.Int64
	// watchdog fires when the connection may have gone quiet.
	watchdog clock.Timer

	mutex sync.Mutex
	// pending holds the requests waiting for a response, by message ID.
	pending map[string]chan protocol.Message
	// closed is set once the connection is closing; no new requests start.
	closed bool
}

func newConnection(hub *Hub, conn *websocket.Conn, agent fleet.Agent, hello protocol.HelloPayload) *connection {
	c := &connection{
		hub: hub, conn: conn, agent: agent, hello: hello,
		watchdog: hub.clock.NewTimer(OfflineAfter),
		pending:  map[string]chan protocol.Message{},
	}
	c.lastActivity.Store(hub.clock.Now().UnixMilli())
	return c
}

// readLoop handles messages until the connection ends.
func (c *connection) readLoop(ctx context.Context) {
	for {
		message, err := protocol.ReadMessage(ctx, c.conn)
		if errors.Is(err, protocol.ErrBinaryFrame) {
			// Streams arrive in a later milestone; nothing uses them yet.
			continue
		}
		if err != nil {
			c.hub.logger.Debug("agent connection ended", "agent", c.agent.PublicID, "error", err)
			return
		}
		c.lastActivity.Store(c.hub.clock.Now().UnixMilli())
		c.handle(ctx, message)
	}
}

func (c *connection) handle(ctx context.Context, message protocol.Message) {
	switch message.Type {
	case protocol.TypePing:
		_ = c.send(ctx, protocol.Message{Type: protocol.TypePong, ID: message.ID}, nil) // a failed send ends the read loop too
	case protocol.TypeResponse:
		c.deliver(message)
	case protocol.TypeEvent:
		c.handleEvent(ctx, message)
	default:
		c.hub.logger.Warn("agent sent an unexpected message", "agent", c.agent.PublicID, "type", message.Type)
	}
}

func (c *connection) handleEvent(ctx context.Context, message protocol.Message) {
	switch message.Action {
	case protocol.ActionMetricsSample:
		var sample protocol.MetricsSample
		if err := protocol.DecodePayload(message, &sample); err != nil {
			c.hub.logger.Warn("agent sent a malformed metrics sample", "agent", c.agent.PublicID, "error", err)
			return
		}
		if err := c.hub.metrics.Ingest(ctx, c.agent.ID, sample); err != nil {
			c.hub.logger.Error("could not store metrics", "agent", c.agent.PublicID, "error", err)
		}
	default:
		c.hub.logger.Debug("agent sent an event this server doesn't handle", "agent", c.agent.PublicID, "action", message.Action)
	}
}

// watchHeartbeat closes the connection when nothing has arrived for
// OfflineAfter, so a vanished agent shows as offline. Its timer is set for
// the earliest moment that could be true; when it fires, the watchdog
// checks how long the connection has really been quiet and sets it again.
func (c *connection) watchHeartbeat(ctx context.Context) {
	defer c.watchdog.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.watchdog.C():
		}
		quiet := c.hub.clock.Now().Sub(time.UnixMilli(c.lastActivity.Load()))
		if quiet >= OfflineAfter {
			c.hub.logger.Info("agent missed its heartbeats; closing its connection", "agent", c.agent.PublicID)
			c.close(protocol.CloseHeartbeatMissed, "nothing received for 45 seconds")
			return
		}
		c.watchdog.Reset(OfflineAfter - quiet)
	}
}

// send writes one message, giving up after writeTimeout.
func (c *connection) send(ctx context.Context, message protocol.Message, payload any) error {
	ctx, cancel := clock.WithTimeout(ctx, c.hub.clock, writeTimeout)
	defer cancel()
	return protocol.WriteMessage(ctx, c.conn, message, payload)
}

// close ends the connection with a close code the agent can act on. It
// returns at once; the closing handshake finishes in the background.
func (c *connection) close(code websocket.StatusCode, reason string) {
	c.mutex.Lock()
	c.closed = true
	c.mutex.Unlock()
	go func() { _ = c.conn.Close(code, reason) }() // best effort; the agent may already be gone
}

// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
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
	// closed is set once the connection is closing; no new requests or
	// streams start.
	closed bool
	// streams holds the open streams, by ID; lastStreamID is the newest ID.
	streams      map[protocol.StreamID]*Stream
	lastStreamID protocol.StreamID
}

func newConnection(hub *Hub, conn *websocket.Conn, agent fleet.Agent, hello protocol.HelloPayload) *connection {
	c := &connection{
		hub: hub, conn: conn, agent: agent, hello: hello,
		watchdog: hub.clock.NewTimer(OfflineAfter),
		pending:  map[string]chan protocol.Message{},
		streams:  map[protocol.StreamID]*Stream{},
	}
	c.lastActivity.Store(hub.clock.Now().UnixMilli())
	return c
}

// readLoop handles messages until the connection ends.
func (c *connection) readLoop(ctx context.Context) {
	for {
		message, frame, err := protocol.ReadAny(ctx, c.conn)
		if err != nil {
			c.hub.logger.Debug("agent connection ended", "agent", c.agent.PublicID, "error", err)
			return
		}
		c.lastActivity.Store(c.hub.clock.Now().UnixMilli())
		if frame != nil {
			c.deliverFrame(ctx, frame)
			continue
		}
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
	case protocol.TypeStreamClose:
		c.handleStreamClose(message)
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
			return
		}
		c.hub.publish(eventFor(c, EventMetrics))
	case protocol.ActionStorageHealth:
		c.handleStorageHealth(ctx, message)
	case protocol.ActionAgentUpdateResult:
		c.handleUpdateResult(ctx, message)
	case protocol.ActionDesktopAvailability:
		c.handleDesktopAvailability(ctx, message)
	case protocol.ActionDockerHealth:
		c.handleDockerHealth(ctx, message)
	case protocol.ActionAgentRemoved:
		c.handleRemoved(ctx)
	default:
		c.hub.logger.Debug("agent sent an event this server doesn't handle", "agent", c.agent.PublicID, "action", message.Action)
	}
}

// handleRemoved marks an uninstalled agent removed and disconnects it,
// which is how the agent knows it was heard.
func (c *connection) handleRemoved(ctx context.Context) {
	if err := c.hub.fleet.MarkRemoved(ctx, c.agent.ID); err != nil {
		c.hub.logger.Error("could not mark an uninstalled agent removed", "agent", c.agent.PublicID, "error", err)
		return
	}
	c.hub.logger.Info("an agent was uninstalled from its machine", "agent", c.agent.PublicID)
	go c.hub.Disconnect(c.agent.ID) // not from the read loop, which Disconnect waits on
}

func (c *connection) handleStorageHealth(ctx context.Context, message protocol.Message) {
	var health protocol.StorageHealthPayload
	if err := protocol.DecodePayload(message, &health); err != nil {
		c.hub.logger.Warn("agent sent malformed storage health", "agent", c.agent.PublicID, "error", err)
		return
	}
	changed, err := c.hub.alerts.RecordStorageHealth(ctx, c.agent.ID, health)
	if err != nil {
		c.hub.logger.Error("could not record storage health", "agent", c.agent.PublicID, "error", err)
		return
	}
	if changed {
		c.hub.publish(eventFor(c, EventAlerts))
	}
}

func (c *connection) handleDockerHealth(ctx context.Context, message protocol.Message) {
	var health protocol.DockerHealthPayload
	if err := protocol.DecodePayload(message, &health); err != nil {
		c.hub.logger.Warn("agent sent malformed container health", "agent", c.agent.PublicID, "error", err)
		return
	}
	changed, err := c.hub.alerts.RecordDockerHealth(ctx, c.agent.ID, health)
	if err != nil {
		c.hub.logger.Error("could not record container health", "agent", c.agent.PublicID, "error", err)
		return
	}
	if changed {
		c.hub.publish(eventFor(c, EventAlerts))
	}
}

func (c *connection) handleUpdateResult(ctx context.Context, message protocol.Message) {
	var result protocol.AgentUpdateResultPayload
	if err := protocol.DecodePayload(message, &result); err != nil || c.hub.updates == nil {
		c.hub.logger.Warn("dropped an agent's update result", "agent", c.agent.PublicID, "error", err)
		return
	}
	if err := c.hub.updates.RecordResult(ctx, c.agent.ID, result); err != nil {
		c.hub.logger.Error("could not record an update result", "agent", c.agent.PublicID, "error", err)
		return
	}
	c.hub.publish(eventFor(c, EventInfoUpdated))
}

// handleDesktopAvailability records that the machine's desktop can now be
// shared, or no longer can, so its page shows or hides the Desktop tab.
func (c *connection) handleDesktopAvailability(ctx context.Context, message protocol.Message) {
	var desktop protocol.DesktopAvailability
	if err := protocol.DecodePayload(message, &desktop); err != nil {
		c.hub.logger.Warn("agent sent a malformed desktop availability", "agent", c.agent.PublicID, "error", err)
		return
	}
	if err := c.hub.fleet.SaveDesktopAvailability(ctx, c.agent.ID, desktop); err != nil {
		c.hub.logger.Error("could not record desktop availability", "agent", c.agent.PublicID, "error", err)
		return
	}
	c.hub.publish(eventFor(c, EventInfoUpdated))
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
// returns at once; the closing handshake finishes in the background while
// the read loop runs down.
func (c *connection) close(code websocket.StatusCode, reason string) {
	c.markClosed()
	go func() { _ = c.conn.Close(code, reason) }() // best effort; the agent may already be gone
}

// closeAndWait is close for when the read loop never started: it waits for
// the closing handshake, because the handler drops the connection as soon
// as it returns.
func (c *connection) closeAndWait(code websocket.StatusCode, reason string) {
	c.markClosed()
	_ = c.conn.Close(code, reason) // best effort; the agent may already be gone
}

func (c *connection) markClosed() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.closed = true
}

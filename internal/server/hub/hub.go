// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/serverkey"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// Timings of the connection (§7.1).
const (
	// HandshakeTimeout is how long enrollment and the handshake may take.
	HandshakeTimeout = 10 * time.Second
	// HeartbeatInterval is how often the agent is told to ping.
	HeartbeatInterval = 15 * time.Second
	// OfflineAfter is how long a connection may stay silent before the
	// server closes it.
	OfflineAfter = 45 * time.Second
	// MetricsInterval is how often the agent is told to send a sample.
	MetricsInterval = 10 * time.Second
	// RequestTimeout is how long Request waits for an agent's response.
	RequestTimeout = 30 * time.Second
	// writeTimeout is how long sending one message may take.
	writeTimeout = 10 * time.Second
)

// Fleet is the agent registry (internal/server/fleet).
type Fleet interface {
	Enroll(ctx context.Context, input fleet.EnrollInput) (fleet.Agent, error)
	AgentForHandshake(ctx context.Context, publicID string) (fleet.Agent, error)
	IsRevoked(ctx context.Context, agentID int64) (bool, error)
	RecordConnected(ctx context.Context, agentID int64, agentVersion string, protocolVersion int) error
	RecordSeen(ctx context.Context, agentID int64) error
	SaveSystemInfo(ctx context.Context, agentID int64, info protocol.SystemInfo) error
}

// Metrics stores metric samples (internal/server/metrics).
type Metrics interface {
	Ingest(ctx context.Context, agentID int64, sample protocol.MetricsSample) error
}

// Alerts raises and resolves alerts from what agents report
// (internal/server/alerts).
type Alerts interface {
	RecordStorageHealth(ctx context.Context, agentID int64, health protocol.StorageHealthPayload) (bool, error)
}

// Updates records how an agent's self-update went (internal/server/updates).
type Updates interface {
	RecordResult(ctx context.Context, agentID int64, result protocol.AgentUpdateResultPayload) error
}

// Clock tells the time and makes timers.
type Clock interface {
	Now() time.Time
	NewTimer(duration time.Duration) clock.Timer
}

// Options are what New needs.
type Options struct {
	Fleet   Fleet
	Metrics Metrics
	// Alerts receives storage health.
	Alerts Alerts
	// Updates receives update results; nil drops them.
	Updates Updates
	Key     serverkey.Key
	Clock   Clock
	Logger  *slog.Logger
}

// Hub accepts agent connections and tracks the live ones.
type Hub struct {
	fleet   Fleet
	metrics Metrics
	alerts  Alerts
	updates Updates
	key     serverkey.Key
	clock   Clock
	logger  *slog.Logger

	mutex sync.Mutex
	// connections holds each online agent's connection, by agent ID.
	connections map[int64]*connection
	// subscribers get an Event whenever something about an agent changes.
	subscribers    map[int]chan Event
	nextSubscriber int
	// shutDown is set by Shutdown; no new connections or subscribers.
	shutDown bool
	// handlers counts the running ServeHTTP calls, so Shutdown can wait.
	handlers sync.WaitGroup
}

// New returns a Hub with no agents connected.
func New(options Options) *Hub {
	return &Hub{
		fleet: options.Fleet, metrics: options.Metrics, alerts: options.Alerts, updates: options.Updates, key: options.Key, clock: options.Clock, logger: options.Logger,
		connections: map[int64]*connection{},
		subscribers: map[int]chan Event{},
	}
}

// IsOnline reports whether the agent is connected right now.
func (h *Hub) IsOnline(agentID int64) bool {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	_, ok := h.connections[agentID]
	return ok
}

// Online lists the agents connected right now, in no particular order.
func (h *Hub) Online() []int64 {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Collect(maps.Keys(h.connections))
}

// Disconnect closes the agent's live connection, if it has one. Revocation
// calls it after the revocation is stored, so a reconnect is refused.
func (h *Hub) Disconnect(agentID int64) {
	h.mutex.Lock()
	conn, ok := h.connections[agentID]
	if ok {
		delete(h.connections, agentID)
		h.publishLocked(eventFor(conn, EventOffline))
	}
	h.mutex.Unlock()
	if ok {
		conn.close(protocol.CloseRevoked, "this agent was revoked")
	}
}

// Shutdown closes every agent connection, telling the agents the server is
// restarting so they reconnect soon, ends every subscription, and waits
// for the connections' handlers to finish. Afterwards new connections are
// refused. Call it before closing the database.
func (h *Hub) Shutdown() {
	h.mutex.Lock()
	h.shutDown = true
	connections := h.connections
	h.connections = map[int64]*connection{}
	h.closeSubscribersLocked()
	h.mutex.Unlock()
	for _, conn := range connections {
		conn.close(websocket.StatusServiceRestart, "the server is restarting")
	}
	h.handlers.Wait()
}

// startHandler counts a new ServeHTTP call, unless the hub is shut down.
func (h *Hub) startHandler() bool {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.shutDown {
		return false
	}
	h.handlers.Add(1)
	return true
}

// register makes conn the agent's live connection, closing any older one:
// an agent that reconnects before the server noticed its old connection
// died must not be shut out. It returns false if the hub is shutting down.
func (h *Hub) register(conn *connection) bool {
	h.mutex.Lock()
	if h.shutDown {
		h.mutex.Unlock()
		return false
	}
	old := h.connections[conn.agent.ID]
	h.connections[conn.agent.ID] = conn
	h.publishLocked(eventFor(conn, EventOnline))
	h.mutex.Unlock()
	if old != nil {
		old.close(protocol.CloseReplaced, "the agent connected again")
	}
	return true
}

// unregister forgets conn, unless a newer connection already replaced it.
func (h *Hub) unregister(conn *connection) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.connections[conn.agent.ID] == conn {
		delete(h.connections, conn.agent.ID)
		h.publishLocked(eventFor(conn, EventOffline))
	}
}

func (h *Hub) connectionOf(agentID int64) (*connection, bool) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	conn, ok := h.connections[agentID]
	return conn, ok
}

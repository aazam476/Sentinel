// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"errors"
	"net/http"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/middleware"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// maxFrameSize is the largest WebSocket message read: one control message.
// Stream frames (a later milestone) stay under it too.
const maxFrameSize = protocol.MaxMessageSize

// ServeHTTP is the /agent/connect endpoint. It upgrades to a WebSocket,
// runs the handshake and then serves the connection until it ends.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Agents never send an Origin header; browsers always do. Refusing
	// browsers outright means no web page can open an agent connection.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "This endpoint is for Sentinel agents only.", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept has already written an error response.
		h.logger.Debug("agent connection upgrade failed", "error", err)
		return
	}
	defer conn.CloseNow() //nolint:errcheck // the connection is over either way
	conn.SetReadLimit(maxFrameSize)

	ip := middleware.ClientInfoFrom(r.Context()).IP.String()
	handshakeCtx, cancel := clock.WithTimeout(r.Context(), h.clock, HandshakeTimeout)
	agent, hello, err := h.handshake(handshakeCtx, conn, ip)
	cancel()
	if err != nil {
		h.closeAfterFailedHandshake(conn, ip, err)
		return
	}
	h.serve(r.Context(), newConnection(h, conn, agent, hello))
}

func (h *Hub) closeAfterFailedHandshake(conn *websocket.Conn, ip string, err error) {
	var refused *closeError
	if !errors.As(err, &refused) {
		// The connection broke or timed out mid-handshake.
		h.logger.Info("agent handshake did not finish", "ip", ip, "error", err)
		return
	}
	h.logger.Warn("agent connection refused", "ip", ip, "reason", refused.reason, "error", refused.err)
	_ = conn.Close(refused.code, refused.reason) // best effort; the agent may already be gone
}

// serve runs an authenticated connection until it ends.
func (h *Hub) serve(ctx context.Context, conn *connection) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	h.register(conn)
	defer h.connectionEnded(conn)
	defer conn.watchdog.Stop()

	// Revocation stores the revocation, then calls Disconnect. If it ran
	// between this agent's handshake and register, Disconnect found
	// nothing to close; this check catches that case.
	revoked, err := h.fleet.IsRevoked(ctx, conn.agent.ID)
	if err != nil {
		h.logger.Error("could not check whether the agent was revoked", "agent", conn.agent.PublicID, "error", err)
		conn.close(websocket.StatusInternalError, "the server couldn't check the agent")
		return
	}
	if revoked {
		conn.close(protocol.CloseRevoked, "this agent was revoked")
		return
	}
	if err := h.fleet.RecordConnected(ctx, conn.agent.ID, conn.hello.AgentVersion, conn.hello.ProtocolVersion); err != nil {
		h.logger.Error("could not record agent connection", "agent", conn.agent.PublicID, "error", err)
	}
	welcome := protocol.WelcomePayload{
		HeartbeatSeconds:       int(HeartbeatInterval.Seconds()),
		MetricsIntervalSeconds: int(MetricsInterval.Seconds()),
	}
	if err := conn.send(ctx, protocol.Message{Type: protocol.TypeWelcome}, welcome); err != nil {
		return
	}
	h.logger.Info("agent connected", "agent", conn.agent.PublicID, "name", conn.agent.Name(), "version", conn.hello.AgentVersion)

	go conn.watchHeartbeat(ctx)
	go h.refreshSystemInfo(ctx, conn)
	conn.readLoop(ctx)
}

func (h *Hub) connectionEnded(conn *connection) {
	conn.failPending()
	// The request is over, so this uses a fresh context. It runs before
	// unregister, so whoever sees the agent go offline also sees when it
	// was last seen.
	if err := h.fleet.RecordSeen(context.Background(), conn.agent.ID); err != nil {
		h.logger.Error("could not record when the agent was last seen", "agent", conn.agent.PublicID, "error", err)
	}
	h.unregister(conn)
	h.logger.Info("agent disconnected", "agent", conn.agent.PublicID, "name", conn.agent.Name())
}

// refreshSystemInfo asks a newly connected agent about its machine and
// stores the answer, so the UI shows current details and capabilities.
func (h *Hub) refreshSystemInfo(ctx context.Context, conn *connection) {
	var info protocol.SystemInfo
	if err := h.request(ctx, conn, protocol.ActionSystemInfo, nil, &info); err != nil {
		h.logger.Warn("could not get the agent's system info", "agent", conn.agent.PublicID, "error", err)
		return
	}
	if err := h.fleet.SaveSystemInfo(ctx, conn.agent.ID, info); err != nil {
		h.logger.Warn("could not save the agent's system info", "agent", conn.agent.PublicID, "error", err)
		return
	}
	h.publish(Event{AgentID: conn.agent.ID, Kind: EventInfoUpdated})
}

// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// ErrServerIdentity means the server's handshake signature didn't verify
// against the key saved at enrollment: whoever answered isn't our server.
// It is never reassigned.
var ErrServerIdentity = errors.New("the server's signature doesn't match the key saved at enrollment")

// Timeouts for one connection.
const (
	// handshakeTimeout covers connecting, TLS and the handshake.
	handshakeTimeout = 10 * time.Second
	writeTimeout     = 10 * time.Second
)

// connection is one live, authenticated connection to the server.
type connection struct {
	agent *Agent
	conn  *websocket.Conn
	// lastActivity is when the server last sent anything, in Unix ms.
	lastActivity atomic.Int64
	// followers maps the streams of followed container logs to the
	// function that stops following.
	followers sync.Map
}

// session connects, runs the handshake and serves the connection until it
// ends. welcomed reports whether the handshake succeeded.
func (a *Agent) session(ctx context.Context) (welcomed bool, err error) {
	// Dialing and the handshake share one deadline, so a network that
	// swallows packets can't hang the agent before it is connected.
	handshakeCtx, cancel := clock.WithTimeout(ctx, a.options.Clock, handshakeTimeout)
	defer cancel()
	a.observe(EventConnecting)
	conn, err := dial(handshakeCtx, a.options.State.ServerURL, a.tlsConfig)
	if err != nil {
		return false, err
	}
	defer conn.CloseNow() //nolint:errcheck // the connection is over either way

	welcome, err := a.handshake(handshakeCtx, conn)
	cancel()
	if err != nil {
		return false, err
	}
	a.options.Logger.Info("connected to the server", "server", a.options.State.ServerURL)
	if seconds := welcome.MetricsIntervalSeconds; seconds > 0 {
		a.metricsInterval.Store(int64(time.Duration(seconds) * time.Second))
	}
	pingInterval := defaultPingInterval
	if welcome.HeartbeatSeconds > 0 {
		pingInterval = time.Duration(welcome.HeartbeatSeconds) * time.Second
	}
	c := &connection{agent: a, conn: conn}
	c.lastActivity.Store(a.options.Clock.Now().UnixMilli())
	return true, c.serve(ctx, pingInterval)
}

// handshake proves the server's identity, then this agent's (§7.3).
func (a *Agent) handshake(ctx context.Context, conn *websocket.Conn) (protocol.WelcomePayload, error) {
	state := a.options.State
	nonce := protocol.NewNonce()
	hello := protocol.HelloPayload{
		AgentID: state.AgentID, AgentVersion: a.options.Version, ProtocolVersion: protocol.CurrentVersion, AgentNonce: nonce,
	}
	if err := protocol.WriteMessage(ctx, conn, protocol.Message{Type: protocol.TypeHello}, hello); err != nil {
		return protocol.WelcomePayload{}, err
	}
	var challenge protocol.ChallengePayload
	if err := protocol.ReadPayload(ctx, conn, protocol.TypeChallenge, &challenge); err != nil {
		return protocol.WelcomePayload{}, fmt.Errorf("handshake: %w", err)
	}
	// The server signs first, so an impostor learns nothing from us.
	if err := protocol.VerifyServerChallenge(state.ServerPublicKey, nonce, challenge.ServerNonce, state.AgentID, challenge.Signature); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "server identity check failed") // best effort
		return protocol.WelcomePayload{}, fmt.Errorf("%w (%w)", ErrServerIdentity, err)
	}
	prove := protocol.ProvePayload{Signature: protocol.SignAgentProof(a.options.Key, challenge.ServerNonce, nonce, state.AgentID)}
	if err := protocol.WriteMessage(ctx, conn, protocol.Message{Type: protocol.TypeProve}, prove); err != nil {
		return protocol.WelcomePayload{}, err
	}
	var welcome protocol.WelcomePayload
	if err := protocol.ReadPayload(ctx, conn, protocol.TypeWelcome, &welcome); err != nil {
		return protocol.WelcomePayload{}, fmt.Errorf("handshake: %w", err)
	}
	return welcome, nil
}

// serve runs the connection's loops until any of them ends it.
func (c *connection) serve(ctx context.Context, pingInterval time.Duration) error {
	// Terminal shells outlive the connection: they wait to be resumed.
	defer c.agent.terminals.DetachAll()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var loops sync.WaitGroup
	defer loops.Wait()
	defer cancel() // runs before Wait: stop the other loops first

	// The timers start before the loops, so the agent is fully running
	// by the time it reports being connected.
	clock := c.agent.options.Clock
	pingTimer, silenceTimer := clock.NewTimer(pingInterval), clock.NewTimer(silenceLimit)
	loops.Go(func() { c.pingForever(ctx, pingTimer, pingInterval); cancel() })
	loops.Go(func() { c.watchSilence(ctx, silenceTimer); cancel() })
	loops.Go(func() { c.sendSamples(ctx); cancel() })
	c.agent.observe(EventConnected)
	return c.readLoop(ctx)
}

// readLoop handles the server's messages until the connection ends.
func (c *connection) readLoop(ctx context.Context) error {
	var requests sync.WaitGroup
	defer requests.Wait()
	for {
		message, frame, err := protocol.ReadAny(ctx, c.conn)
		if err != nil {
			return err
		}
		c.lastActivity.Store(c.agent.options.Clock.Now().UnixMilli())
		switch {
		case frame != nil:
			c.handleFrame(frame)
		case message.Type == protocol.TypeStreamClose:
			c.handleStreamClose(message)
		case message.Type == protocol.TypeRequest:
			requests.Go(func() { c.answer(ctx, message) })
		}
	}
}

func (c *connection) pingForever(ctx context.Context, timer clock.Timer, interval time.Duration) {
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C():
		}
		if c.send(ctx, protocol.Message{Type: protocol.TypePing, ID: protocol.NewID(c.agent.options.Clock.Now())}, nil) != nil {
			return
		}
		timer.Reset(interval)
	}
}

// watchSilence closes a connection the server has stopped answering, so
// the agent reconnects instead of waiting on a dead one.
func (c *connection) watchSilence(ctx context.Context, timer clock.Timer) {
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C():
		}
		quiet := c.agent.options.Clock.Now().Sub(time.UnixMilli(c.lastActivity.Load()))
		if quiet >= silenceLimit {
			c.agent.options.Logger.Info("the server stopped answering; reconnecting")
			_ = c.conn.CloseNow() // ends the read loop
			return
		}
		timer.Reset(silenceLimit - quiet)
	}
}

// sendSamples sends buffered samples, oldest first, whenever there are any.
// A sample leaves the buffer only once it was sent.
func (c *connection) sendSamples(ctx context.Context) {
	buffer := c.agent.buffer
	for {
		for {
			sample, ok := buffer.oldest()
			if !ok {
				break
			}
			event := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(c.agent.options.Clock.Now()), Action: protocol.ActionMetricsSample}
			if c.send(ctx, event, sample) != nil {
				return
			}
			buffer.remove(sample)
		}
		select {
		case <-ctx.Done():
			return
		case <-buffer.added:
		}
	}
}

// send writes one message, giving up after writeTimeout.
func (c *connection) send(ctx context.Context, message protocol.Message, payload any) error {
	ctx, cancel := clock.WithTimeout(ctx, c.agent.options.Clock, writeTimeout)
	defer cancel()
	return protocol.WriteMessage(ctx, c.conn, message, payload)
}

// SPDX-License-Identifier: Apache-2.0

package hub_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
)

// agentID is the internal ID of a test agent.
func (h *harness) agentID(t *testing.T, agent *testAgent) int64 {
	t.Helper()
	stored, err := h.fleet.AgentForHandshake(t.Context(), agent.id)
	if err != nil {
		t.Fatal(err)
	}
	return stored.ID
}

func TestSEC21_AnEnrolledAgentAndTheServerProveTheirIdentities(t *testing.T) {
	t.Run("enrollment returns the agent's ID and the server's key", func(t *testing.T) {
		h := newHarness(t)
		agent := h.dial(t)
		agent.enroll(h.newToken(t))
		if !protocol.IsID(agent.id) || !agent.serverKey.Equal(h.key.Public()) {
			t.Errorf("enrolled as %q with server key %x", agent.id, agent.serverKey)
		}
		if !protocol.FingerprintMatches(h.key.Fingerprint(), agent.serverKey) {
			t.Error("the key doesn't match the fingerprint an install command would carry")
		}
	})

	t.Run("after both signatures check out, the agent is welcomed and online", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		id := h.agentID(t, agent)
		waitForEvent(t, events, hub.Event{AgentID: id, Kind: hub.EventOnline})
		if !h.hub.IsOnline(id) {
			t.Error("the agent isn't online")
		}
	})

	t.Run("welcome tells the agent its heartbeat and metrics intervals", func(t *testing.T) {
		h := newHarness(t)
		agent := h.reconnect(t, h.enrolledAgent(t))
		welcome := agent.handshake()
		if welcome.HeartbeatSeconds != 15 || welcome.MetricsIntervalSeconds != 10 {
			t.Errorf("welcome = %+v", welcome)
		}
	})

	t.Run("a new agent can carry on to the handshake on its enrollment connection", func(t *testing.T) {
		h := newHarness(t)
		agent := h.dial(t)
		agent.enroll(h.newToken(t))
		agent.handshake()
	})

	t.Run("the server's signature fails against any other key, so an impostor is detected", func(t *testing.T) {
		h := newHarness(t)
		agent := h.reconnect(t, h.enrolledAgent(t))
		challenge := agent.hello()
		impostor, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := protocol.VerifyServerChallenge(impostor, agent.nonce, challenge.ServerNonce, agent.id, challenge.Signature); err == nil {
			t.Error("the challenge verified against a key the agent never saved")
		}
	})

	t.Run("an agent signing with the wrong key is refused", func(t *testing.T) {
		h := newHarness(t)
		agent := h.reconnect(t, h.enrolledAgent(t))
		challenge := agent.hello()
		_, stolen, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		agent.prove(challenge, stolen)
		if got := agent.closeStatus(); got != protocol.CloseHandshakeFailed {
			t.Errorf("close status = %d, want CloseHandshakeFailed", got)
		}
		if h.hub.IsOnline(h.agentID(t, agent)) {
			t.Error("the impostor is online")
		}
	})

	t.Run("a proof for another connection's nonce is refused", func(t *testing.T) {
		h := newHarness(t)
		agent := h.reconnect(t, h.enrolledAgent(t))
		challenge := agent.hello()
		challenge.ServerNonce = protocol.NewNonce() // as if replayed from an earlier handshake
		agent.prove(challenge, agent.key)
		if got := agent.closeStatus(); got != protocol.CloseHandshakeFailed {
			t.Errorf("close status = %d, want CloseHandshakeFailed", got)
		}
	})
}

func TestSEC21_RevocationClosesTheLiveConnectionImmediately(t *testing.T) {
	h := newHarness(t)
	events := h.subscribe(t)
	agent := h.online(t)
	id := h.agentID(t, agent)
	waitForEvent(t, events, hub.Event{AgentID: id, Kind: hub.EventOnline})

	if _, err := h.fleet.RevokeAgent(t.Context(), h.admin, agent.id, auth.RequestInfo{}); err != nil {
		t.Fatal(err)
	}
	h.hub.Disconnect(id)

	if h.hub.IsOnline(id) {
		t.Error("the agent is still online right after Disconnect")
	}
	waitForEvent(t, events, hub.Event{AgentID: id, Kind: hub.EventOffline})
	if got := agent.closeStatus(); got != protocol.CloseRevoked {
		t.Errorf("close status = %d, want CloseRevoked", got)
	}

	again := h.reconnect(t, agent)
	again.send(protocol.Message{Type: protocol.TypeHello}, protocol.HelloPayload{
		AgentID: agent.id, AgentVersion: "v1", ProtocolVersion: protocol.CurrentVersion, AgentNonce: protocol.NewNonce(),
	})
	if got := again.closeStatus(); got != protocol.CloseRevoked {
		t.Errorf("reconnect close status = %d, want CloseRevoked", got)
	}
}

func TestHandshakeRefusals(t *testing.T) {
	t.Run("a wrong enrollment token is refused with its own close code", func(t *testing.T) {
		h := newHarness(t)
		agent := h.dial(t)
		agent.send(protocol.Message{Type: protocol.TypeEnroll}, protocol.EnrollPayload{
			Token: "guess", PublicKey: agent.key.Public().(ed25519.PublicKey), Hostname: "x", OS: "linux", Arch: "amd64",
			AgentVersion: "v1", ProtocolVersion: protocol.CurrentVersion,
		})
		if got := agent.closeStatus(); got != protocol.CloseEnrollmentRefused {
			t.Errorf("close status = %d, want CloseEnrollmentRefused", got)
		}
	})

	t.Run("an enrollment with an invalid hostname is refused", func(t *testing.T) {
		h := newHarness(t)
		agent := h.dial(t)
		agent.send(protocol.Message{Type: protocol.TypeEnroll}, protocol.EnrollPayload{
			Token: h.newToken(t), PublicKey: agent.key.Public().(ed25519.PublicKey), Hostname: "", OS: "linux",
			Arch: "amd64", AgentVersion: "v1", ProtocolVersion: protocol.CurrentVersion,
		})
		if got := agent.closeStatus(); got != protocol.CloseHandshakeFailed {
			t.Errorf("close status = %d, want CloseHandshakeFailed", got)
		}
	})

	t.Run("an unknown agent is told to enroll again", func(t *testing.T) {
		h := newHarness(t)
		agent := h.dial(t)
		agent.id = protocol.NewID(h.clock.Now())
		agent.send(protocol.Message{Type: protocol.TypeHello}, protocol.HelloPayload{
			AgentID: agent.id, AgentVersion: "v1", ProtocolVersion: protocol.CurrentVersion, AgentNonce: protocol.NewNonce(),
		})
		if got := agent.closeStatus(); got != protocol.CloseUnknownAgent {
			t.Errorf("close status = %d, want CloseUnknownAgent", got)
		}
	})

	versions := []struct {
		name    string
		message protocol.MessageType
		payload func(agent *testAgent, token string) any
	}{
		{"hello", protocol.TypeHello, func(agent *testAgent, _ string) any {
			return protocol.HelloPayload{AgentID: agent.id, AgentVersion: "v9", ProtocolVersion: protocol.CurrentVersion + 1, AgentNonce: protocol.NewNonce()}
		}},
		{"enroll", protocol.TypeEnroll, func(agent *testAgent, token string) any {
			return protocol.EnrollPayload{Token: token, PublicKey: agent.key.Public().(ed25519.PublicKey), Hostname: "x",
				OS: "linux", Arch: "amd64", AgentVersion: "v0", ProtocolVersion: protocol.OldestSupportedVersion - 1}
		}},
	}
	for _, version := range versions {
		t.Run("a protocol version the server doesn't speak is refused in "+version.name, func(t *testing.T) {
			h := newHarness(t)
			agent := h.dial(t)
			agent.send(protocol.Message{Type: version.message}, version.payload(agent, h.newToken(t)))
			if got := agent.closeStatus(); got != protocol.CloseUnsupportedVersion {
				t.Errorf("close status = %d, want CloseUnsupportedVersion", got)
			}
		})
	}

	malformed := []struct {
		name string
		send func(agent *testAgent)
	}{
		{"a first message that is neither enroll nor hello", func(agent *testAgent) {
			agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		}},
		{"a hello without a payload", func(agent *testAgent) {
			agent.send(protocol.Message{Type: protocol.TypeHello}, nil)
		}},
		{"an enroll without a payload", func(agent *testAgent) {
			agent.send(protocol.Message{Type: protocol.TypeEnroll}, nil)
		}},
		{"a hello with a short nonce", func(agent *testAgent) {
			agent.send(protocol.Message{Type: protocol.TypeHello}, protocol.HelloPayload{
				AgentID: "x", AgentVersion: "v1", ProtocolVersion: protocol.CurrentVersion, AgentNonce: []byte("short"),
			})
		}},
		{"text that isn't a message", func(agent *testAgent) {
			if err := agent.conn.Write(agent.t.Context(), websocket.MessageText, []byte("{")); err != nil {
				agent.t.Fatal(err)
			}
		}},
	}
	for _, test := range malformed {
		t.Run(test.name+" is refused", func(t *testing.T) {
			h := newHarness(t)
			agent := h.dial(t)
			test.send(agent)
			if got := agent.closeStatus(); got != protocol.CloseHandshakeFailed {
				t.Errorf("close status = %d, want CloseHandshakeFailed", got)
			}
		})
	}

	t.Run("an enrolled agent that sends something other than hello is refused", func(t *testing.T) {
		h := newHarness(t)
		agent := h.dial(t)
		agent.enroll(h.newToken(t))
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		if got := agent.closeStatus(); got != protocol.CloseHandshakeFailed {
			t.Errorf("close status = %d, want CloseHandshakeFailed", got)
		}
	})

	t.Run("an answer to the challenge that isn't prove is refused", func(t *testing.T) {
		h := newHarness(t)
		agent := h.reconnect(t, h.enrolledAgent(t))
		agent.hello()
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		if got := agent.closeStatus(); got != protocol.CloseHandshakeFailed {
			t.Errorf("close status = %d, want CloseHandshakeFailed", got)
		}
	})

	t.Run("an agent that doesn't finish the handshake in time is dropped", func(t *testing.T) {
		h := newHarness(t)
		agent := h.dial(t)
		if err := h.clock.WaitForTimers(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		h.clock.Advance(hub.HandshakeTimeout)
		if got := agent.closeStatus(); got == websocket.StatusNormalClosure {
			t.Errorf("close status = %d, want the connection dropped", got)
		}
	})

	t.Run("a browser is refused before the upgrade", func(t *testing.T) {
		h := newHarness(t)
		if got := rejectedStatus(t, h.url, http.Header{"Origin": {"https://evil.example"}}); got != http.StatusForbidden {
			t.Errorf("status = %d, want 403", got)
		}
	})

	t.Run("a plain HTTP request is refused", func(t *testing.T) {
		h := newHarness(t)
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http"+h.url[len("ws"):], nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close() // nothing to read
		if response.StatusCode == http.StatusSwitchingProtocols || response.StatusCode < 400 {
			t.Errorf("status = %d, want an error", response.StatusCode)
		}
	})
}

func TestHandshakeServerFailures(t *testing.T) {
	tests := []struct {
		method  string
		connect func(h *harness, t *testing.T) *testAgent
	}{
		{"Enroll", func(h *harness, t *testing.T) *testAgent {
			agent := h.dial(t)
			agent.send(protocol.Message{Type: protocol.TypeEnroll}, protocol.EnrollPayload{
				Token: h.newToken(t), PublicKey: agent.key.Public().(ed25519.PublicKey), Hostname: "x", OS: "linux",
				Arch: "amd64", AgentVersion: "v1", ProtocolVersion: protocol.CurrentVersion,
			})
			return agent
		}},
		{"AgentForHandshake", func(h *harness, t *testing.T) *testAgent {
			agent := h.reconnect(t, h.enrolledAgent(t))
			agent.send(protocol.Message{Type: protocol.TypeHello}, protocol.HelloPayload{
				AgentID: agent.id, AgentVersion: "v1", ProtocolVersion: protocol.CurrentVersion, AgentNonce: protocol.NewNonce(),
			})
			return agent
		}},
		{"IsRevoked", func(h *harness, t *testing.T) *testAgent {
			agent := h.reconnect(t, h.enrolledAgent(t))
			agent.prove(agent.hello(), agent.key)
			return agent
		}},
	}
	for _, test := range tests {
		t.Run("a failure in "+test.method+" closes the connection as an internal error", func(t *testing.T) {
			h := newHarness(t)
			h.faults.breakMethod(test.method)
			agent := test.connect(h, t)
			if got := agent.closeStatus(); got != websocket.StatusInternalError {
				t.Errorf("close status = %d, want StatusInternalError", got)
			}
		})
	}

	t.Run("an agent revoked during its handshake is closed after all", func(t *testing.T) {
		h := newHarness(t)
		agent := h.reconnect(t, h.enrolledAgent(t))
		challenge := agent.hello()
		// The revocation lands after the server looked the agent up but
		// before the connection is registered.
		if _, err := h.fleet.RevokeAgent(t.Context(), h.admin, agent.id, auth.RequestInfo{}); err != nil {
			t.Fatal(err)
		}
		agent.prove(challenge, agent.key)
		if got := agent.closeStatus(); got != protocol.CloseRevoked {
			t.Errorf("close status = %d, want CloseRevoked", got)
		}
	})

	t.Run("failing to record the connection or last seen doesn't stop the agent", func(t *testing.T) {
		h := newHarness(t)
		h.faults.breakMethod("RecordConnected")
		h.faults.breakMethod("RecordSeen")
		events := h.subscribe(t)
		agent := h.online(t)
		id := h.agentID(t, agent)
		waitForEvent(t, events, hub.Event{AgentID: id, Kind: hub.EventOnline})
		_ = agent.conn.Close(websocket.StatusNormalClosure, "")
		waitForEvent(t, events, hub.Event{AgentID: id, Kind: hub.EventOffline})
	})
}

func TestConnection(t *testing.T) {
	t.Run("answers a ping with a pong carrying its ID", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		id := protocol.NewID(h.clock.Now())
		agent.send(protocol.Message{Type: protocol.TypePing, ID: id}, nil)
		if pong := agent.read(); pong.Type != protocol.TypePong || pong.ID != id {
			t.Errorf("got %+v, want a pong for %s", pong, id)
		}
	})

	t.Run("stores the system info the agent reports after welcome", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{
			Hostname: "nas-01.home", OS: "linux", Arch: "arm64",
			Capabilities: []protocol.Capability{{Name: "terminal", Enabled: false, Reason: "Disabled by local policy"}},
		})
		waitForEvent(t, events, hub.Event{AgentID: h.agentID(t, agent), Kind: hub.EventInfoUpdated})
		stored, err := h.fleet.GetAgent(t.Context(), h.admin, agent.id)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Hostname != "nas-01.home" || len(stored.SystemInfo.Capabilities) != 1 || stored.AgentVersion != "v0.1.1" {
			t.Errorf("stored = %+v", stored)
		}
	})

	t.Run("stores metric samples", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionMetricsSample},
			protocol.MetricsSample{Time: h.clock.Now().UnixMilli(), CPUPercent: 42})
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		agent.read()
		snapshot, ok := h.metrics.Latest(h.agentID(t, agent))
		if !ok || snapshot.Values[metrics.CPUPercent] != 42 {
			t.Errorf("snapshot = %+v (%v)", snapshot, ok)
		}
	})

	t.Run("raises storage alerts from storage health, and ignores malformed health", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		id := h.agentID(t, agent)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		event := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionStorageHealth}
		agent.send(event, map[string]string{"pools": "many"})
		agent.send(event, protocol.StorageHealthPayload{Pools: []protocol.PoolHealth{{Name: "tank", Health: "DEGRADED"}}})
		waitForEvent(t, events, hub.Event{AgentID: id, PublicID: agent.id, Kind: hub.EventAlerts})
		if counts, err := h.alerts.FiringCounts(t.Context()); err != nil || counts[id] != 1 {
			t.Errorf("counts %v, %v", counts, err)
		}
	})

	t.Run("ignores malformed samples, unknown events and unexpected messages", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		event := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionMetricsSample}
		agent.send(event, map[string]string{"cpu": "lots"})
		event.Action = "docker.container.state_changed"
		agent.send(event, map[string]string{})
		agent.send(protocol.Message{Type: protocol.TypeWelcome}, nil)
		agent.send(protocol.Message{Type: protocol.TypeResponse, ID: protocol.NewID(h.clock.Now())}, nil) // answers nothing
		if err := agent.conn.Write(t.Context(), websocket.MessageBinary, protocol.EncodeFrame(1, []byte("x"))); err != nil {
			t.Fatal(err)
		}
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		if pong := agent.read(); pong.Type != protocol.TypePong {
			t.Errorf("got %s, want the connection still working", pong.Type)
		}
	})

	t.Run("closes a connection that goes quiet for 45 seconds", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		id := h.agentID(t, agent)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		waitForEvent(t, events, hub.Event{AgentID: id, Kind: hub.EventOnline})
		h.settle(t, 1) // the heartbeat watchdog
		h.clock.Advance(hub.OfflineAfter)
		if got := agent.closeStatus(); got != protocol.CloseHeartbeatMissed {
			t.Errorf("close status = %d, want CloseHeartbeatMissed", got)
		}
		waitForEvent(t, events, hub.Event{AgentID: id, Kind: hub.EventOffline})
		stored, err := h.fleet.GetAgent(t.Context(), h.admin, agent.id)
		if err != nil || stored.LastSeenAt == nil || *stored.LastSeenAt != h.clock.Now().UnixMilli() {
			t.Errorf("last seen = %v (%v), want now", stored.LastSeenAt, err)
		}
	})

	t.Run("a ping keeps a connection alive", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		for range 4 { // nearly three minutes, pinging every 40 seconds
			h.settle(t, 1) // the heartbeat watchdog
			h.clock.Advance(40 * time.Second)
			agent.send(protocol.Message{Type: protocol.TypePing}, nil)
			if pong := agent.read(); pong.Type != protocol.TypePong {
				t.Fatalf("got %s, want a pong", pong.Type)
			}
		}
	})

	t.Run("a second connection from the same agent replaces the first", func(t *testing.T) {
		h := newHarness(t)
		first := h.online(t)
		second := h.reconnect(t, first)
		second.handshake()
		if got := first.closeStatus(); got != protocol.CloseReplaced {
			t.Errorf("close status = %d, want CloseReplaced", got)
		}
		second.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		second.send(protocol.Message{Type: protocol.TypePing}, nil)
		second.read()
		if !h.hub.IsOnline(h.agentID(t, second)) {
			t.Error("the agent went offline when its old connection closed")
		}
	})
}

func TestRequest(t *testing.T) {
	t.Run("returns the agent's answer", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		result := make(chan error, 1)
		var info protocol.SystemInfo
		go func() {
			result <- h.hub.Request(t.Context(), h.agentID(t, agent), protocol.ActionSystemInfo, nil, &info)
		}()
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "answered"})
		if err := <-result; err != nil || info.Hostname != "answered" {
			t.Errorf("Request = %+v, %v", info, err)
		}
	})

	t.Run("returns the agent's error", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		result := make(chan error, 1)
		go func() {
			result <- h.hub.Request(t.Context(), h.agentID(t, agent), "docker.container.restart", nil, nil)
		}()
		request := agent.read()
		agent.send(protocol.Message{Type: protocol.TypeResponse, ID: request.ID,
			Error: &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "Docker is disabled by local policy"}}, nil)
		var agentErr *protocol.Error
		if err := <-result; !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied {
			t.Errorf("err = %v, want policy_denied", err)
		}
	})

	t.Run("accepts an answer without a payload when none is wanted", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		result := make(chan error, 1)
		go func() { result <- h.hub.Request(t.Context(), h.agentID(t, agent), "system.reboot", nil, nil) }()
		agent.send(protocol.Message{Type: protocol.TypeResponse, ID: agent.read().ID}, nil)
		if err := <-result; err != nil {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("times out cleanly when the agent doesn't answer", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		result := make(chan error, 1)
		go func() { result <- h.hub.Request(t.Context(), h.agentID(t, agent), "system.reboot", nil, nil) }()
		agent.read()   // the request arrives; the agent sits on it
		h.settle(t, 2) // the heartbeat watchdog and the request's timeout
		h.clock.Advance(hub.RequestTimeout)
		if err := <-result; !errors.Is(err, hub.ErrTimeout) {
			t.Errorf("err = %v, want ErrTimeout", err)
		}
		// The connection is still fine.
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		if pong := agent.read(); pong.Type != protocol.TypePong {
			t.Errorf("got %s after the timeout, want a pong", pong.Type)
		}
	})

	t.Run("fails cleanly when the agent disconnects before answering", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		result := make(chan error, 1)
		go func() { result <- h.hub.Request(t.Context(), h.agentID(t, agent), "system.reboot", nil, nil) }()
		agent.read()
		_ = agent.conn.CloseNow()
		if err := <-result; !errors.Is(err, hub.ErrDisconnected) {
			t.Errorf("err = %v, want ErrDisconnected", err)
		}
	})

	t.Run("refuses an agent that isn't connected", func(t *testing.T) {
		h := newHarness(t)
		if err := h.hub.Request(t.Context(), 1, protocol.ActionSystemInfo, nil, nil); !errors.Is(err, hub.ErrOffline) {
			t.Errorf("err = %v, want ErrOffline", err)
		}
	})

	t.Run("stops waiting when the caller gives up", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		ctx, cancel := contextWithCancel(t)
		result := make(chan error, 1)
		go func() { result <- h.hub.Request(ctx, h.agentID(t, agent), "system.reboot", nil, nil) }()
		agent.read()
		cancel()
		if err := <-result; err == nil || errors.Is(err, hub.ErrTimeout) {
			t.Errorf("err = %v, want the cancellation", err)
		}
	})

	t.Run("a system info answer the server can't store is logged, not fatal", func(t *testing.T) {
		h := newHarness(t)
		h.faults.breakMethod("SaveSystemInfo")
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		if pong := agent.read(); pong.Type != protocol.TypePong {
			t.Errorf("got %s, want the connection still working", pong.Type)
		}
	})

	t.Run("a system info request the agent fails is logged, not fatal", func(t *testing.T) {
		h := newHarness(t)
		agent := h.online(t)
		request := agent.read()
		agent.send(protocol.Message{Type: protocol.TypeResponse, ID: request.ID,
			Error: &protocol.Error{Code: protocol.ErrorInternal, Message: "no"}}, nil)
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		if pong := agent.read(); pong.Type != protocol.TypePong {
			t.Errorf("got %s, want the connection still working", pong.Type)
		}
	})
}

func TestDisconnect(t *testing.T) {
	t.Run("does nothing for an agent that isn't connected", func(t *testing.T) {
		h := newHarness(t)
		h.hub.Disconnect(42)
	})
}

func TestShutdown(t *testing.T) {
	t.Run("tells connected agents the server is restarting and ends subscriptions", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		waitForEvent(t, events, hub.Event{AgentID: h.agentID(t, agent), Kind: hub.EventOnline})

		done := make(chan struct{})
		go func() { h.hub.Shutdown(); close(done) }()
		if got := agent.closeStatus(); got != websocket.StatusServiceRestart {
			t.Errorf("close status = %d, want StatusServiceRestart", got)
		}
		<-done
		for range events { // drains, then sees the channel closed
		}
		if h.hub.IsOnline(h.agentID(t, agent)) {
			t.Error("the agent is online after shutdown")
		}
	})

	t.Run("refuses new connections and subscriptions afterwards", func(t *testing.T) {
		h := newHarness(t)
		h.hub.Shutdown()
		if got := rejectedStatus(t, h.url, nil); got != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", got)
		}
		events, stop := h.hub.Subscribe()
		defer stop()
		if _, open := <-events; open {
			t.Error("a subscription after shutdown is open")
		}
	})

	t.Run("an agent mid-handshake when the server stops is turned away", func(t *testing.T) {
		h := newHarness(t)
		agent := h.reconnect(t, h.enrolledAgent(t))
		challenge := agent.hello()
		done := make(chan struct{})
		go func() { h.hub.Shutdown(); close(done) }()
		// Shutdown waits for this handler; once the proof arrives the
		// handler finds the hub shut down and closes.
		agent.prove(challenge, agent.key)
		if got := agent.closeStatus(); got != websocket.StatusServiceRestart {
			t.Errorf("close status = %d, want StatusServiceRestart", got)
		}
		<-done
	})
}

func TestUpdateResults(t *testing.T) {
	t.Run("records an agent's update result and announces it", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		result := protocol.AgentUpdateResultPayload{Version: "v1.1.0", RolledBack: true, Error: "didn't connect"}
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionAgentUpdateResult}, result)
		if got := <-h.updates.results; got != result {
			t.Errorf("recorded %+v", got)
		}
		waitForEvent(t, events, hub.Event{AgentID: h.agentID(t, agent), PublicID: agent.id, Kind: hub.EventInfoUpdated})
	})

	t.Run("drops a malformed result, and one it can't record", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionAgentUpdateResult}, "not a result")
		h.updates.fail = true
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionAgentUpdateResult}, protocol.AgentUpdateResultPayload{Version: "v1"})
		// The connection carries on: a later event is still handled.
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionMetricsSample},
			protocol.MetricsSample{Time: h.clock.Now().UnixMilli(), CPUPercent: 1})
		waitForEvent(t, events, hub.Event{AgentID: h.agentID(t, agent), PublicID: agent.id, Kind: hub.EventMetrics})
		select {
		case got := <-h.updates.results:
			t.Errorf("recorded %+v", got)
		default:
		}
	})
}

func TestEvents(t *testing.T) {
	t.Run("announces alerts raised away from the agent's connection", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		h.hub.AlertsChanged(7, "01JBX3K5Q7Z8M4N6P8R0T2V4W0")
		waitForEvent(t, events, hub.Event{AgentID: 7, PublicID: "01JBX3K5Q7Z8M4N6P8R0T2V4W0", Kind: hub.EventAlerts})
	})

	t.Run("a stored sample is announced with the agent's public ID", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionMetricsSample},
			protocol.MetricsSample{Time: h.clock.Now().UnixMilli(), CPUPercent: 1})
		for event := range events {
			if event.Kind == hub.EventMetrics {
				if event.PublicID != agent.id {
					t.Errorf("event for %q, want %q", event.PublicID, agent.id)
				}
				return
			}
		}
	})
}

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(t.Context())
}

func TestDesktopAvailability(t *testing.T) {
	t.Run("records that the desktop can now be shared, and announces it", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01", Desktop: &protocol.DesktopAvailability{Reason: "No one is logged in."}})
		waitForEvent(t, events, hub.Event{AgentID: h.agentID(t, agent), PublicID: agent.id, Kind: hub.EventInfoUpdated})
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionDesktopAvailability},
			protocol.DesktopAvailability{Available: true})
		waitForEvent(t, events, hub.Event{AgentID: h.agentID(t, agent), PublicID: agent.id, Kind: hub.EventInfoUpdated})
		stored, err := h.fleet.GetAgent(t.Context(), h.admin, agent.id)
		if err != nil {
			t.Fatal(err)
		}
		if desktop := stored.SystemInfo.Desktop; desktop == nil || !desktop.Available || stored.SystemInfo.Hostname != "nas-01" {
			t.Errorf("stored = %+v", stored.SystemInfo)
		}
	})

	t.Run("drops a malformed availability, and one with text it won't store", func(t *testing.T) {
		h := newHarness(t)
		events := h.subscribe(t)
		agent := h.online(t)
		agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
		waitForEvent(t, events, hub.Event{AgentID: h.agentID(t, agent), PublicID: agent.id, Kind: hub.EventInfoUpdated})
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionDesktopAvailability}, "not an availability")
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionDesktopAvailability},
			protocol.DesktopAvailability{Reason: "bell\x07"})
		// The connection carries on: a later event is still handled.
		agent.send(protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(h.clock.Now()), Action: protocol.ActionMetricsSample},
			protocol.MetricsSample{Time: h.clock.Now().UnixMilli(), CPUPercent: 1})
		waitForEvent(t, events, hub.Event{AgentID: h.agentID(t, agent), PublicID: agent.id, Kind: hub.EventMetrics})
		stored, err := h.fleet.GetAgent(t.Context(), h.admin, agent.id)
		if err != nil || stored.SystemInfo.Desktop != nil {
			t.Errorf("stored = %+v (%v)", stored.SystemInfo.Desktop, err)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"crypto/ed25519"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestEnroll(t *testing.T) {
	t.Run("returns the state to save once the server's key matches the fingerprint", func(t *testing.T) {
		s := newServer(t)
		state, err := core.Enroll(t.Context(), s.enrollOptions(newKey(t)))
		if err != nil {
			t.Fatal(err)
		}
		if !protocol.IsID(state.AgentID) || !state.ServerPublicKey.Equal(s.key.Public()) ||
			state.ServerURL != s.url() || state.TLSFingerprint != s.tlsPin() {
			t.Errorf("state = %+v", state)
		}
	})

	t.Run("refuses a server whose key doesn't match the fingerprint", func(t *testing.T) {
		s := newServer(t)
		options := s.enrollOptions(newKey(t))
		_, impostor, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		options.Fingerprint = protocol.Fingerprint(impostor.Public().(ed25519.PublicKey))
		if _, err := core.Enroll(t.Context(), options); !errors.Is(err, core.ErrWrongServer) {
			t.Errorf("err = %v, want ErrWrongServer", err)
		}
	})

	t.Run("refuses a TLS certificate that doesn't carry the pinned key", func(t *testing.T) {
		s := newServer(t)
		options := s.enrollOptions(newKey(t))
		options.TLSFingerprint = "SHA256:" + strings.Repeat("A", 43)
		if _, err := core.Enroll(t.Context(), options); err == nil || !strings.Contains(err.Error(), "pinned") {
			t.Errorf("err = %v, want a pinning failure", err)
		}
	})

	t.Run("without a pin, checks the certificate like a browser and refuses a self-signed one", func(t *testing.T) {
		s := newServer(t)
		options := s.enrollOptions(newKey(t))
		options.TLSFingerprint = ""
		if _, err := core.Enroll(t.Context(), options); err == nil {
			t.Error("Enroll trusted a self-signed certificate without a pin")
		}
	})

	t.Run("says when the token is refused", func(t *testing.T) {
		s := newServer(t)
		options := s.enrollOptions(newKey(t))
		options.Token = "wrong"
		if _, err := core.Enroll(t.Context(), options); !errors.Is(err, core.ErrEnrollmentRefused) {
			t.Errorf("err = %v, want ErrEnrollmentRefused", err)
		}
	})

	t.Run("reports a server it can't reach or a URL it can't use", func(t *testing.T) {
		s := newServer(t)
		for _, url := range []string{"https://127.0.0.1:1", "ftp://example.com", "::"} {
			options := s.enrollOptions(newKey(t))
			options.ServerURL = url
			if _, err := core.Enroll(t.Context(), options); err == nil {
				t.Errorf("%s: Enroll returned nil", url)
			}
		}
	})
}

func TestRun(t *testing.T) {
	t.Run("connects, reports the machine with the policy's capabilities, and sends samples", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		localPolicy, err := policy.Parse("[capabilities]\nterminal = false\n")
		if err != nil {
			t.Fatal(err)
		}
		agent := s.startAgent(localPolicy)
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)

		stored, err := s.fleet.GetAgent(t.Context(), s.admin, agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.AgentVersion != "v0.1.1" || len(stored.SystemInfo.Capabilities) != len(policy.Capabilities()) {
			t.Fatalf("stored = %+v", stored)
		}
		for _, capability := range stored.SystemInfo.Capabilities {
			if capability.Name == policy.Terminal && (capability.Enabled || capability.Reason != policy.DisabledReason) {
				t.Errorf("terminal = %+v, want disabled by local policy", capability)
			}
		}

		agent.clock.Advance(10 * time.Second)
		agent.expect(t, core.EventSampled)
		waitFor(t, events, agent.state.AgentID, hub.EventMetrics)
	})

	t.Run("answers an action it doesn't know with unknown_action", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		err = s.hub.Request(t.Context(), stored.ID, "clipboard.read", nil, nil)
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorUnknownAction {
			t.Errorf("err = %v, want unknown_action", err)
		}
	})

	t.Run("refuses an impostor server and never proves its identity to it", func(t *testing.T) {
		s := newServer(t)
		key := newKey(t)
		state, err := core.Enroll(t.Context(), s.enrollOptions(key))
		if err != nil {
			t.Fatal(err)
		}
		_, impostor, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		// As if the server's key had been swapped: the agent's saved key
		// no longer matches what signs the challenge.
		state.ServerPublicKey = impostor.Public().(ed25519.PublicKey)
		agent := s.runAgent(state, key, policy.Policy{})
		agent.expect(t, core.EventWaiting) // the handshake failed; it waits to retry
		stored, err := s.fleet.AgentForHandshake(t.Context(), state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		if s.hub.IsOnline(stored.ID) {
			t.Error("the agent connected to a server that failed its identity check")
		}
	})
}

func TestRunEdgeCases(t *testing.T) {
	t.Run("answers system.info with an error when it can't read the machine", func(t *testing.T) {
		s := newServer(t)
		key := newKey(t)
		state, err := core.Enroll(t.Context(), s.enrollOptions(key))
		if err != nil {
			t.Fatal(err)
		}
		agent := s.runAgentWith(state, key, policy.Policy{}, &fakeCollector{failInfo: true})
		agent.expect(t, core.EventConnected)
		stored, err := s.fleet.AgentForHandshake(t.Context(), state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		var agentErr *protocol.Error
		if err := s.hub.Request(t.Context(), stored.ID, protocol.ActionSystemInfo, nil, nil); !errors.As(err, &agentErr) ||
			agentErr.Code != protocol.ErrorInternal {
			t.Errorf("err = %v, want internal", err)
		}
	})

	t.Run("keeps a connection the server is still using", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		agent.clock.Advance(30 * time.Second)
		// The request reaches the agent at 30 seconds; the watchdog,
		// checking at 45, sees 15 quiet seconds and keeps the connection.
		if err := s.hub.Request(t.Context(), stored.ID, protocol.ActionSystemInfo, nil, nil); err != nil {
			t.Fatal(err)
		}
		agent.clock.Advance(15 * time.Second)
		if err := s.hub.Request(t.Context(), stored.ID, protocol.ActionSystemInfo, nil, nil); err != nil {
			t.Errorf("the connection was dropped: %v", err)
		}
	})
}

// The fault-injection scenarios of §13.4.
func TestReconnection(t *testing.T) {
	t.Run("a connection attempt into a dead network times out and is retried", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		agent.expect(t, core.EventConnected)

		s.proxy.Stall(true)
		s.proxy.Drop()
		agent.expect(t, core.EventWaiting)
		agent.clock.Advance(2 * time.Second)
		agent.expect(t, core.EventConnecting) // into the stalled network
		agent.clock.Advance(10 * time.Second)
		agent.expect(t, core.EventSampled, core.EventWaiting) // the attempt gave up
		s.proxy.Stall(false)
		s.proxy.Drop()
		agent.clock.Advance(time.Minute)
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventOnline)
	})

	t.Run("reconnects after a network drop and delivers the samples taken meanwhile", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)

		s.proxy.Refuse(true)
		s.proxy.Drop()
		agent.expect(t, core.EventWaiting)
		waitFor(t, events, agent.state.AgentID, hub.EventOffline)
		for range 3 { // half a minute offline: a sample and a failed retry every 10 seconds
			agent.clock.Advance(10 * time.Second)
			agent.expect(t, core.EventSampled, core.EventWaiting)
		}
		s.proxy.Refuse(false)
		agent.clock.Advance(time.Minute) // past the next retry
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventOnline)

		for range 3 {
			waitFor(t, events, agent.state.AgentID, hub.EventMetrics)
		}
		stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		now := agent.clock.Now()
		series, err := s.metrics.Query(t.Context(), stored.ID, metrics.CPUPercent, now.Add(-time.Hour).UnixMilli(), now.UnixMilli()+1)
		if err != nil || len(series.Points) < 3 {
			t.Errorf("stored %d points (%v), want the 3 buffered samples", len(series.Points), err)
		}
	})

	t.Run("reconnects after the server restarts", func(t *testing.T) {
		s := newServer(t)
		agent := s.startAgent(policy.Policy{})
		agent.expect(t, core.EventConnected)

		s.restart()
		events := s.subscribe()
		agent.expect(t, core.EventWaiting)
		agent.clock.Advance(2 * time.Second)
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventOnline)
	})

	t.Run("gives up on a connection that goes silent and reconnects", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)

		s.proxy.Stall(true)
		agent.clock.Advance(45 * time.Second) // no answer to any ping
		agent.expect(t, core.EventWaiting)    // the watchdog dropped the dead connection
		s.proxy.Stall(false)
		s.proxy.Drop() // the server's side of the old connection, which it would notice later
		agent.clock.Advance(time.Minute)
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventOnline)
	})

	t.Run("keeps trying after being revoked, and is refused each time", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		revoked, err := s.fleet.RevokeAgent(t.Context(), s.admin, agent.state.AgentID, auth.RequestInfo{})
		if err != nil {
			t.Fatal(err)
		}
		s.hub.Disconnect(revoked.ID)
		agent.expect(t, core.EventWaiting)
		for range 3 {
			agent.clock.Advance(time.Minute)
			agent.expect(t, core.EventConnecting, core.EventWaiting)
		}
		if s.hub.IsOnline(revoked.ID) {
			t.Error("a revoked agent is online")
		}
	})
}

func TestConnectURL(t *testing.T) {
	tests := []struct{ server, want string }{
		{"https://sentinel.example.com", "wss://sentinel.example.com/agent/connect"},
		{"https://example.com/sentinel/", "wss://example.com/sentinel/agent/connect"},
		{"http://10.0.0.5:8443?x=1#y", "ws://10.0.0.5:8443/agent/connect"},
	}
	for _, test := range tests {
		t.Run("maps "+test.server, func(t *testing.T) {
			got, err := core.ConnectURL(test.server)
			if err != nil || got != test.want {
				t.Errorf("got %q (%v), want %q", got, err, test.want)
			}
		})
	}
}

// TestSEC26_TheAgentNeverListensOnANetworkPort checks the agent's source:
// no code under internal/agent or cmd/sentinel-agent opens a listening
// socket, except the tray's local Unix socket (ADR 0038), which must say
// so. The agent only dials out to the network.
func TestSEC26_TheAgentNeverListensOnANetworkPort(t *testing.T) {
	listening := regexp.MustCompile(`net\.Listen(TCP|UDP|Unix|Packet)?\(|ListenConfig|ListenPacket|ListenAndServe|httptest\.`)
	// The one place allowed to listen, and the call it must make there.
	const traySocket, trayCall = "agent/tray/listen.go", `config.Listen(context.Background(), "unix", path)`
	for _, root := range []string{"../../agent", "../../../cmd/sentinel-agent"} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			source, err := os.ReadFile(path) //nolint:gosec // G304: walking this repository's own source
			if err != nil {
				return err
			}
			if filepath.ToSlash(path) == "../../"+traySocket {
				if len(listening.FindAll(source, -1)) != 1 || !strings.Contains(string(source), trayCall) {
					t.Errorf("%s may only listen on a Unix socket, with %s", path, trayCall)
				}
				return nil
			}
			if match := listening.Find(source); match != nil {
				t.Errorf("%s calls %s; the agent must never listen", path, match)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestReportRemovedRevokesTheAgentOnTheServer(t *testing.T) {
	s := newServer(t)
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := core.Enroll(t.Context(), s.enrollOptions(key))
	if err != nil {
		t.Fatal(err)
	}
	agent := core.New(core.Options{
		State: state, Key: key, Collector: &fakeCollector{}, Clock: fakeclock.New(s.clock.Now()),
		Logger: slog.New(slog.DiscardHandler), Version: "v0.1.1", Storage: &fakeStorage{},
	})
	if err := agent.ReportRemoved(t.Context()); err != nil {
		t.Fatalf("ReportRemoved = %v", err)
	}
	if _, err := s.fleet.AgentForHandshake(t.Context(), state.AgentID); !errors.Is(err, fleet.ErrAgentRevoked) {
		t.Errorf("AgentForHandshake = %v, want the uninstalled machine revoked", err)
	}
	if err := agent.ReportRemoved(t.Context()); err == nil {
		t.Error("a revoked agent's report succeeded; the server should refuse its handshake")
	}
}

// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/identity"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/terminal"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
	"github.com/aazam476/sentinel/internal/server/serverkey"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
	"github.com/aazam476/sentinel/internal/testutil/faultnet"
)

// server is the part of a Sentinel server that agents talk to, on a real
// database, served over TLS behind a fault-injecting proxy. It can be
// restarted on the same database.
type server struct {
	t       *testing.T
	dbPath  string
	key     serverkey.Key
	clock   *fakeclock.Clock
	proxy   *faultnet.Proxy
	store   *store.Store
	fleet   *fleet.Service
	metrics *metrics.Service
	alerts  *alerts.Service
	hub     *hub.Hub
	http    *httptest.Server
	admin   auth.Session
	// shells starts agents' terminal shells; tests that open terminals set
	// it to a fake.
	shells terminal.Starter
	// machine holds fakes for the parts that act on the machine (Services,
	// Logs, Processes, Power); only those fields are used.
	machine core.Options
}

func newServer(t *testing.T) *server {
	t.Helper()
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{
		t: t, dbPath: filepath.Join(t.TempDir(), "sentinel.db"), key: serverkey.FromPrivateKey(private),
		clock: fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)),
	}
	s.start()
	s.proxy = faultnet.New(t, s.http.Listener.Addr().String())
	s.admin = s.addAdmin()
	t.Cleanup(s.stop)
	return s
}

// start opens the database and serves the agent endpoint.
func (s *server) start() {
	s.t.Helper()
	database, err := store.Open(s.t.Context(), s.dbPath, s.clock)
	if err != nil {
		s.t.Fatal(err)
	}
	s.store = database
	s.fleet = fleet.NewService(fleet.Options{Database: database, AuditLog: audit.New(database), Clock: s.clock})
	s.metrics = metrics.NewService(database, s.clock)
	s.alerts = alerts.NewService(database, s.clock)
	s.hub = hub.New(hub.Options{
		Fleet: s.fleet, Metrics: s.metrics, Alerts: s.alerts, Key: s.key, Clock: s.clock, Logger: slog.New(slog.DiscardHandler),
	})
	mux := http.NewServeMux()
	mux.Handle(core.AgentPath, s.hub)
	s.http = httptest.NewUnstartedServer(mux)
	// Tests that refuse the certificate on purpose make the server log TLS
	// errors; they aren't failures.
	s.http.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.http.StartTLS()
}

// stop shuts the server down as a restart would.
func (s *server) stop() {
	if s.http == nil {
		return
	}
	s.hub.Shutdown()
	s.http.Close()
	_ = s.store.Close() // nothing to check when a test database closes
	s.http = nil
}

// restart stops the server and starts it again on the same database, on a
// new port; the proxy follows it, as DNS would.
func (s *server) restart() {
	s.t.Helper()
	s.stop()
	s.start()
	s.proxy.SetTarget(s.http.Listener.Addr().String())
}

func (s *server) addAdmin() auth.Session {
	s.t.Helper()
	var id int64
	err := s.store.Write(s.t.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(s.t.Context(), `INSERT INTO users (username, password_hash, role, created_at) VALUES ('alice', 'x', 'admin', 0)`)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		s.t.Fatal(err)
	}
	signedIn := int64(0)
	return auth.Session{UserID: id, Username: "alice", Role: auth.RoleAdmin, SecondFactorAt: &signedIn}
}

// url is the server's address as agents see it: through the proxy.
func (s *server) url() string {
	return "https://" + s.proxy.Addr()
}

// tlsPin is the fingerprint of the test server certificate's key, as an
// install command for a self-signed server would carry.
func (s *server) tlsPin() string {
	return protocol.CertificateKeyFingerprint(s.http.Certificate())
}

func (s *server) newToken() string {
	s.t.Helper()
	token, err := s.fleet.CreateEnrollmentToken(s.t.Context(), s.admin, fleet.CreateTokenInput{}, auth.RequestInfo{})
	if err != nil {
		s.t.Fatal(err)
	}
	return token.Token
}

// enrollOptions are a typical install command's.
func (s *server) enrollOptions(key ed25519.PrivateKey) core.EnrollOptions {
	return core.EnrollOptions{
		ServerURL: s.url(), Token: s.newToken(), Fingerprint: s.key.Fingerprint(), TLSFingerprint: s.tlsPin(),
		Key: key, Hostname: "nas-01", OS: "linux", Arch: "amd64", AgentVersion: "v0.1.0",
	}
}

// subscribe returns the hub's events. After a restart, subscribe again.
func (s *server) subscribe() <-chan hub.Event {
	events, stop := s.hub.Subscribe()
	s.t.Cleanup(stop)
	return events
}

func waitFor(t *testing.T, events <-chan hub.Event, agentID string, kind hub.EventKind) {
	t.Helper()
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatalf("the hub shut down before a %s event", kind)
			}
			if event.PublicID == agentID && event.Kind == kind {
				return
			}
		case <-t.Context().Done():
			t.Fatalf("no %s event", kind)
		}
	}
}

// fakeCollector reports a sample with an increasing CPU figure, so tests
// can tell samples apart.
type fakeCollector struct {
	taken atomic.Int64
	// failInfo makes SystemInfo fail, as on a machine it can't read.
	failInfo bool
}

func (c *fakeCollector) Sample(_ context.Context, now time.Time) protocol.MetricsSample {
	return protocol.MetricsSample{Time: now.UnixMilli(), CPUPercent: float64(c.taken.Add(1))}
}

func (c *fakeCollector) SystemInfo(_ context.Context, capabilities []protocol.Capability, version string) (protocol.SystemInfo, error) {
	if c.failInfo {
		return protocol.SystemInfo{}, errors.New("can't read this machine")
	}
	return protocol.SystemInfo{Hostname: "nas-01", OS: "linux", Arch: "amd64", AgentVersion: version, Capabilities: capabilities}, nil
}

// runningAgent is an enrolled agent running in the background.
type runningAgent struct {
	state identity.State
	clock *fakeclock.Clock
	// events receives what the agent reports (core.Options.Observe).
	events chan core.Event
}

// startAgent enrolls a new agent and runs it until the test ends.
func (s *server) startAgent(localPolicy policy.Policy) *runningAgent {
	s.t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		s.t.Fatal(err)
	}
	state, err := core.Enroll(s.t.Context(), s.enrollOptions(key))
	if err != nil {
		s.t.Fatal(err)
	}
	return s.runAgent(state, key, localPolicy)
}

func (s *server) runAgent(state identity.State, key ed25519.PrivateKey, localPolicy policy.Policy) *runningAgent {
	s.t.Helper()
	return s.runAgentWith(state, key, localPolicy, &fakeCollector{})
}

func (s *server) runAgentWith(state identity.State, key ed25519.PrivateKey, localPolicy policy.Policy, collector core.Collector) *runningAgent {
	s.t.Helper()
	if s.machine.Storage == nil {
		// Without a fake, the agent would read this machine's disks.
		s.machine.Storage = &fakeStorage{}
	}
	running := &runningAgent{state: state, clock: fakeclock.New(s.clock.Now()), events: make(chan core.Event, 1000)}
	agent := core.New(core.Options{
		State: state, Key: key, Policy: localPolicy, Collector: collector, Clock: running.clock,
		Logger: slog.New(slog.DiscardHandler), Version: "v0.1.1", Random: func() float64 { return 0.5 },
		Observe:  func(event core.Event) { running.events <- event },
		Shells:   s.shells,
		Services: s.machine.Services, Logs: s.machine.Logs, Processes: s.machine.Processes, Power: s.machine.Power,
		Docker: s.machine.Docker, Stacks: s.machine.Stacks, Storage: s.machine.Storage, Files: s.machine.Files,
	})
	ctx, cancel := context.WithCancel(s.t.Context())
	done := make(chan struct{})
	go func() { agent.Run(ctx); close(done) }()
	s.t.Cleanup(func() { cancel(); <-done })
	return running
}

// expect waits until the agent has reported each of the events, in any
// order, skipping others. Only then is it safe to move the fake clock:
// every timer the agent waits on is running.
func (a *runningAgent) expect(t *testing.T, wanted ...core.Event) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for len(wanted) > 0 {
		select {
		case event := <-a.events:
			if index := slices.Index(wanted, event); index >= 0 {
				wanted = slices.Delete(wanted, index, index+1)
			}
		case <-ctx.Done():
			t.Fatalf("the agent never reported %v", wanted)
		}
	}
}

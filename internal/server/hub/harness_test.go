// SPDX-License-Identifier: Apache-2.0

package hub_test

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
	"github.com/aazam476/sentinel/internal/server/middleware"
	"github.com/aazam476/sentinel/internal/server/serverkey"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// harness is a Hub on a real database, served over a real WebSocket, with
// a fake clock for its timeouts.
type harness struct {
	hub     *hub.Hub
	fleet   *fleet.Service
	faults  *faultyFleet
	metrics *metrics.Service
	clock   *fakeclock.Clock
	key     serverkey.Key
	url     string
	admin   auth.Session
}

func newHarness(t testing.TB) *harness {
	t.Helper()
	clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() }) // nothing to check when a test database closes

	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		fleet:   fleet.NewService(fleet.Options{Database: database, AuditLog: audit.New(database), Clock: clock}),
		metrics: metrics.NewService(database, clock),
		clock:   clock,
		key:     serverkey.FromPrivateKey(private),
		admin:   addAdmin(t, database),
	}
	h.faults = &faultyFleet{Fleet: h.fleet}
	h.hub = hub.New(hub.Options{Fleet: h.faults, Metrics: h.metrics, Key: h.key, Clock: clock, Logger: slog.New(slog.DiscardHandler)})
	server := httptest.NewServer(middleware.WithClientInfo(h.hub, nil))
	t.Cleanup(server.Close)
	h.url = "ws" + strings.TrimPrefix(server.URL, "http")
	return h
}

func addAdmin(t testing.TB, database *store.Store) auth.Session {
	t.Helper()
	var id int64
	err := database.Write(t.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(t.Context(), `INSERT INTO users (username, password_hash, role, created_at) VALUES ('alice', 'x', 'admin', 0)`)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	signedInAt := int64(0)
	return auth.Session{UserID: id, Username: "alice", Role: auth.RoleAdmin, SecondFactorAt: &signedInAt}
}

func (h *harness) newToken(t *testing.T) string {
	t.Helper()
	token, err := h.fleet.CreateEnrollmentToken(t.Context(), h.admin, fleet.CreateTokenInput{}, auth.RequestInfo{})
	if err != nil {
		t.Fatal(err)
	}
	return token.Token
}

// testAgent plays the agent's side of the protocol, one step at a time, so
// tests can also break the rules.
type testAgent struct {
	t         *testing.T
	conn      *websocket.Conn
	key       ed25519.PrivateKey
	id        string
	serverKey ed25519.PublicKey
	nonce     []byte
}

func (h *harness) dial(t *testing.T) *testAgent {
	t.Helper()
	conn, _, err := websocket.Dial(t.Context(), h.url, nil) //nolint:bodyclose // coder/websocket closes the response body
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() }) // may already be closed
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &testAgent{t: t, conn: conn, key: key}
}

// enrolledAgent returns a new agent that enrolled and then disconnected.
func (h *harness) enrolledAgent(t *testing.T) *testAgent {
	t.Helper()
	agent := h.dial(t)
	agent.enroll(h.newToken(t))
	_ = agent.conn.Close(websocket.StatusNormalClosure, "")
	return agent
}

// reconnect opens a new connection for the same agent.
func (h *harness) reconnect(t *testing.T, agent *testAgent) *testAgent {
	t.Helper()
	next := h.dial(t)
	next.key, next.id, next.serverKey = agent.key, agent.id, agent.serverKey
	return next
}

// online returns an agent that finished the handshake and was welcomed.
func (h *harness) online(t *testing.T) *testAgent {
	t.Helper()
	agent := h.reconnect(t, h.enrolledAgent(t))
	agent.handshake()
	return agent
}

func (a *testAgent) send(message protocol.Message, payload any) {
	a.t.Helper()
	if err := protocol.WriteMessage(a.t.Context(), a.conn, message, payload); err != nil {
		a.t.Fatal(err)
	}
}

func (a *testAgent) read() protocol.Message {
	a.t.Helper()
	message, err := protocol.ReadMessage(a.t.Context(), a.conn)
	if err != nil {
		a.t.Fatalf("read: %v", err)
	}
	return message
}

func (a *testAgent) readPayload(want protocol.MessageType, destination any) {
	a.t.Helper()
	if err := protocol.ReadPayload(a.t.Context(), a.conn, want, destination); err != nil {
		a.t.Fatalf("read %s: %v", want, err)
	}
}

func (a *testAgent) enroll(token string) {
	a.t.Helper()
	a.send(protocol.Message{Type: protocol.TypeEnroll}, protocol.EnrollPayload{
		Token: token, PublicKey: a.key.Public().(ed25519.PublicKey), Hostname: "nas-01", OS: "linux", Arch: "amd64",
		AgentVersion: "v0.1.0", ProtocolVersion: protocol.CurrentVersion,
	})
	var enrolled protocol.EnrolledPayload
	a.readPayload(protocol.TypeEnrolled, &enrolled)
	a.id, a.serverKey = enrolled.AgentID, enrolled.ServerPublicKey
}

// hello sends hello and returns the server's challenge.
func (a *testAgent) hello() protocol.ChallengePayload {
	a.t.Helper()
	a.nonce = protocol.NewNonce()
	a.send(protocol.Message{Type: protocol.TypeHello}, protocol.HelloPayload{
		AgentID: a.id, AgentVersion: "v0.1.1", ProtocolVersion: protocol.CurrentVersion, AgentNonce: a.nonce,
	})
	var challenge protocol.ChallengePayload
	a.readPayload(protocol.TypeChallenge, &challenge)
	return challenge
}

func (a *testAgent) prove(challenge protocol.ChallengePayload, key ed25519.PrivateKey) {
	a.t.Helper()
	a.send(protocol.Message{Type: protocol.TypeProve}, protocol.ProvePayload{
		Signature: protocol.SignAgentProof(key, challenge.ServerNonce, a.nonce, a.id),
	})
}

// handshake runs hello, checks the server's signature, proves and reads
// welcome.
func (a *testAgent) handshake() protocol.WelcomePayload {
	a.t.Helper()
	challenge := a.hello()
	if err := protocol.VerifyServerChallenge(a.serverKey, a.nonce, challenge.ServerNonce, a.id, challenge.Signature); err != nil {
		a.t.Fatalf("server signature: %v", err)
	}
	a.prove(challenge, a.key)
	var welcome protocol.WelcomePayload
	a.readPayload(protocol.TypeWelcome, &welcome)
	return welcome
}

// answerSystemInfo reads the request the server sends after welcome and
// answers it.
func (a *testAgent) answerSystemInfo(info protocol.SystemInfo) {
	a.t.Helper()
	request := a.read()
	if request.Type != protocol.TypeRequest || request.Action != protocol.ActionSystemInfo {
		a.t.Fatalf("got %s %s, want the system.info request", request.Type, request.Action)
	}
	a.send(protocol.Message{Type: protocol.TypeResponse, ID: request.ID}, info)
}

// closeStatus waits for the server to close the connection and returns
// its close code.
func (a *testAgent) closeStatus() websocket.StatusCode {
	a.t.Helper()
	for {
		_, err := protocol.ReadMessage(a.t.Context(), a.conn)
		if errors.Is(err, protocol.ErrBinaryFrame) {
			continue
		}
		if err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

// subscribe returns the hub's events for the rest of the test.
func (h *harness) subscribe(t *testing.T) <-chan hub.Event {
	t.Helper()
	events, stop := h.hub.Subscribe()
	t.Cleanup(stop)
	return events
}

// settle waits until exactly the given number of the fake clock's timers
// are armed. A message the server sent can reach the test agent before the
// server has disarmed that write's timeout; advancing the clock in that gap
// would fire it, and the connection would close as if the write had hung.
func (h *harness) settle(t *testing.T, timers int) {
	t.Helper()
	if err := h.clock.WaitForArmed(t.Context(), timers); err != nil {
		t.Fatal(err)
	}
}

// waitForEvent waits for a particular event.
func waitForEvent(t *testing.T, events <-chan hub.Event, want hub.Event) {
	t.Helper()
	for {
		select {
		case event := <-events:
			if event.AgentID == want.AgentID && event.Kind == want.Kind {
				return
			}
		case <-t.Context().Done():
			t.Fatalf("no event %+v", want)
		}
	}
}

// errInjected is what faultyFleet returns when told to fail.
var errInjected = errors.New("injected failure")

// faultyFleet passes calls to the real fleet service, failing the ones a
// test switches off.
type faultyFleet struct {
	hub.Fleet
	mutex sync.Mutex
	fail  map[string]bool
}

func (f *faultyFleet) failing(method string) bool {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.fail[method]
}

func (f *faultyFleet) breakMethod(method string) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.fail == nil {
		f.fail = map[string]bool{}
	}
	f.fail[method] = true
}

func (f *faultyFleet) Enroll(ctx context.Context, input fleet.EnrollInput) (fleet.Agent, error) {
	if f.failing("Enroll") {
		return fleet.Agent{}, errInjected
	}
	return f.Fleet.Enroll(ctx, input)
}

func (f *faultyFleet) AgentForHandshake(ctx context.Context, publicID string) (fleet.Agent, error) {
	if f.failing("AgentForHandshake") {
		return fleet.Agent{}, errInjected
	}
	return f.Fleet.AgentForHandshake(ctx, publicID)
}

func (f *faultyFleet) IsRevoked(ctx context.Context, agentID int64) (bool, error) {
	if f.failing("IsRevoked") {
		return false, errInjected
	}
	return f.Fleet.IsRevoked(ctx, agentID)
}

func (f *faultyFleet) RecordConnected(ctx context.Context, agentID int64, version string, protocolVersion int) error {
	if f.failing("RecordConnected") {
		return errInjected
	}
	return f.Fleet.RecordConnected(ctx, agentID, version, protocolVersion)
}

func (f *faultyFleet) RecordSeen(ctx context.Context, agentID int64) error {
	if f.failing("RecordSeen") {
		return errInjected
	}
	return f.Fleet.RecordSeen(ctx, agentID)
}

func (f *faultyFleet) SaveSystemInfo(ctx context.Context, agentID int64, info protocol.SystemInfo) error {
	if f.failing("SaveSystemInfo") {
		return errInjected
	}
	return f.Fleet.SaveSystemInfo(ctx, agentID, info)
}

// rejectedStatus is the HTTP status of a request the endpoint refuses
// before upgrading.
func rejectedStatus(t *testing.T, url string, header http.Header) int {
	t.Helper()
	_, response, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: header})
	if err == nil {
		t.Fatal("the connection was accepted")
	}
	if response == nil {
		t.Fatalf("no HTTP response: %v", err)
	}
	defer func() { _ = response.Body.Close() }() // nothing left to read
	return response.StatusCode
}

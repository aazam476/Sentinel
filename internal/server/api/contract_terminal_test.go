// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/relay"
)

// terminalFixture is a signed-in admin and an online agent whose policy
// allows the terminal.
type terminalFixture struct {
	server  *testServer
	admin   admin
	agentID string
	agent   *websocket.Conn
}

func newTerminalFixture(t *testing.T) terminalFixture {
	t.Helper()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	events, stop := server.app.Hub.Subscribe()
	defer stop()
	token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
	agentID, agent := server.connectAgentWith(token.Secret, token.ServerFingerprint,
		[]protocol.Capability{{Name: "terminal", Enabled: true}})
	waitForAgentEvent(t, events, agentID, hub.EventInfoUpdated)
	return terminalFixture{server: server, admin: admin, agentID: agentID, agent: agent}
}

// dialTerminal opens the terminal WebSocket as browser would. status is the
// HTTP status when the upgrade is refused.
func (f terminalFixture) dialTerminal(t *testing.T, as *browser, query string) (conn *websocket.Conn, status int, err error) {
	t.Helper()
	url := "wss" + strings.TrimPrefix(f.server.server.URL, "https") + "/api/agents/" + f.agentID + "/terminal" + query
	conn, response, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{
		HTTPClient: as.client, HTTPHeader: http.Header{"Origin": {f.server.publicOrigin}},
	})
	if response != nil {
		status = response.StatusCode
		if response.Body != nil {
			_ = response.Body.Close() // nothing to read
		}
	}
	if conn != nil {
		t.Cleanup(func() { _ = conn.CloseNow() })
	}
	return conn, status, err
}

// acceptOpen plays the agent's side of terminal.open.
func (f terminalFixture) acceptOpen(t *testing.T, sessionID string, resumed bool) protocol.TerminalOpenPayload {
	t.Helper()
	request := f.agentRead(t, protocol.TypeRequest)
	if request.Action != protocol.ActionTerminalOpen {
		t.Fatalf("agent got %s", request.Action)
	}
	var open protocol.TerminalOpenPayload
	if err := protocol.DecodePayload(request, &open); err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeResponse, ID: request.ID},
		protocol.TerminalOpenedPayload{SessionID: sessionID, Resumed: resumed}); err != nil {
		t.Fatal(err)
	}
	return open
}

// agentRead reads the agent's next control message of the given type.
func (f terminalFixture) agentRead(t *testing.T, want protocol.MessageType) protocol.Message {
	t.Helper()
	for {
		message, frame, err := protocol.ReadAny(t.Context(), f.agent)
		if err != nil {
			t.Fatal(err)
		}
		if frame == nil && message.Type == want {
			return message
		}
	}
}

func browserStatus(t *testing.T, conn *websocket.Conn) relay.Status {
	t.Helper()
	kind, data, err := conn.Read(t.Context())
	if err != nil || kind != websocket.MessageText {
		t.Fatalf("read status: %v %v", kind, err)
	}
	var status relay.Status
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatal(err)
	}
	return status
}

// eventualAuditActions waits up to five seconds for the audit entries with
// prefix to be want, for actions audited just after their response ends,
// such as a download, and returns what it last saw.
func eventualAuditActions(t *testing.T, server *testServer, prefix string, want []string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := auditActions(t, server, prefix)
		if slices.Equal(got, want) || time.Now().After(deadline) {
			return got
		}
		runtime.Gosched()
	}
}

func auditActions(t *testing.T, server *testServer, prefix string) []string {
	t.Helper()
	records, err := server.app.AuditLog.List(t.Context(), audit.ListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for i := len(records) - 1; i >= 0; i-- {
		if strings.HasPrefix(records[i].Action, prefix) {
			actions = append(actions, records[i].Action+":"+string(records[i].Outcome))
		}
	}
	return actions
}

func TestTerminal(t *testing.T) {
	t.Run("relays a shell session and audits its start and end", func(t *testing.T) {
		f := newTerminalFixture(t)
		browser, _, err := f.dialTerminal(t, f.admin.browser, "?cols=132&rows=43")
		if err != nil {
			t.Fatal(err)
		}
		open := f.acceptOpen(t, "s1", false)
		if open.Cols != 132 || open.Rows != 43 {
			t.Errorf("agent was asked for %dx%d", open.Cols, open.Rows)
		}
		if status := browserStatus(t, browser); status.Type != "opened" || status.SessionID != "s1" {
			t.Errorf("status = %+v", status)
		}
		if err := protocol.WriteFrame(t.Context(), f.agent, open.StreamID, []byte("$ ")); err != nil {
			t.Fatal(err)
		}
		if _, data, err := browser.Read(t.Context()); err != nil || string(data) != "$ " {
			t.Errorf("browser got %q (%v)", data, err)
		}
		if err := browser.Write(t.Context(), websocket.MessageBinary, []byte("ls\r")); err != nil {
			t.Fatal(err)
		}
		for {
			_, frame, err := protocol.ReadAny(t.Context(), f.agent)
			if err != nil {
				t.Fatal(err)
			}
			if frame != nil {
				if _, data, _ := protocol.DecodeFrame(frame); string(data) != "ls\r" {
					t.Errorf("agent got %q", data)
				}
				break
			}
		}

		if err := browser.Write(t.Context(), websocket.MessageText, []byte(`{"type":"resize","cols":80,"rows":24}`)); err != nil {
			t.Fatal(err)
		}
		resize := f.agentRead(t, protocol.TypeRequest)
		var size protocol.TerminalResizePayload
		if err := protocol.DecodePayload(resize, &size); err != nil || size.Cols != 80 || size.StreamID != open.StreamID {
			t.Errorf("resize = %+v (%v)", size, err)
		}
		_ = protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeResponse, ID: resize.ID}, nil)

		if err := browser.Write(t.Context(), websocket.MessageText, []byte(`{"type":"close"}`)); err != nil {
			t.Fatal(err)
		}
		var closed protocol.StreamClosePayload
		if err := protocol.DecodePayload(f.agentRead(t, protocol.TypeStreamClose), &closed); err != nil || closed.KeepSession {
			t.Errorf("close = %+v (%v), want a final close", closed, err)
		}
		// The handler audits the end before it returns and drops the
		// connection, so once the browser sees the connection end, the
		// entry is there.
		var ended error
		for ended == nil {
			_, _, ended = browser.Read(t.Context())
		}
		want := []string{"terminal.open:started", "terminal.close:success"}
		if got := auditActions(t, f.server, "terminal."); !slices.Equal(got, want) {
			t.Errorf("audit = %v, want %v (the browser's connection ended with: %v)", got, want, ended)
		}
	})

	t.Run("a dropped browser can resume its session, and no one else can", func(t *testing.T) {
		f := newTerminalFixture(t)
		browser, _, err := f.dialTerminal(t, f.admin.browser, "")
		if err != nil {
			t.Fatal(err)
		}
		f.acceptOpen(t, "s1", false)
		browserStatus(t, browser)
		_ = browser.CloseNow()
		var closed protocol.StreamClosePayload
		if err := protocol.DecodePayload(f.agentRead(t, protocol.TypeStreamClose), &closed); err != nil || !closed.KeepSession {
			t.Fatalf("close = %+v (%v), want keepSession", closed, err)
		}

		other := f.server.addUser(f.admin, "olu", "operator")
		if _, status, err := f.dialTerminal(t, other, "?resume=s1"); err == nil || status != http.StatusNotFound {
			t.Errorf("another user resumed: %v", err)
		}
		again, _, err := f.dialTerminal(t, f.admin.browser, "?resume=s1")
		if err != nil {
			t.Fatal(err)
		}
		if open := f.acceptOpen(t, "s1", true); open.ResumeSessionID != "s1" {
			t.Errorf("agent asked to resume %q", open.ResumeSessionID)
		}
		if status := browserStatus(t, again); !status.Resumed {
			t.Errorf("status = %+v, want resumed", status)
		}
	})

	t.Run("the agent's refusal is audited as denied and shown to the browser", func(t *testing.T) {
		f := newTerminalFixture(t)
		browser, _, err := f.dialTerminal(t, f.admin.browser, "")
		if err != nil {
			t.Fatal(err)
		}
		request := f.agentRead(t, protocol.TypeRequest)
		_ = protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeResponse, ID: request.ID,
			Error: &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "The terminal is disabled by local policy."}}, nil)
		_, _, err = browser.Read(t.Context())
		if websocket.CloseStatus(err) != websocket.StatusPolicyViolation || !strings.Contains(err.Error(), "disabled by local policy") {
			t.Errorf("browser got %v", err)
		}
		if got := auditActions(t, f.server, "terminal."); !slices.Equal(got, []string{"terminal.open:denied"}) {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestSEC07_OpeningATerminalNeedsARecentSecondFactor(t *testing.T) {
	f := newTerminalFixture(t)
	f.server.clock.Advance(16 * time.Minute)
	_, status, err := f.dialTerminal(t, f.admin.browser, "")
	if err == nil || status != http.StatusForbidden {
		t.Fatalf("dial = %v, want 403", err)
	}
	expect(t, f.admin.browser.do(http.MethodPost, "/api/auth/step-up", api.TOTPCodeRequest{TOTPCode: f.server.code(f.admin.totpSecret)}), http.StatusOK, "")
	if _, _, err := f.dialTerminal(t, f.admin.browser, ""); err != nil {
		t.Errorf("after step-up: %v", err)
	}
}

func TestSEC13_ATerminalWebSocketFromAnotherSiteIsRefused(t *testing.T) {
	f := newTerminalFixture(t)
	url := "wss" + strings.TrimPrefix(f.server.server.URL, "https") + "/api/agents/" + f.agentID + "/terminal"
	_, response, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{
		HTTPClient: f.admin.browser.client, HTTPHeader: http.Header{"Origin": {"https://evil.example"}},
	})
	if err == nil {
		t.Fatal("a page on another site opened a terminal")
	}
	_ = response.Body.Close() // nothing to read
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", response.StatusCode)
	}
}

func TestTerminalPreflight(t *testing.T) {
	t.Run("a plain GET runs the checks without opening a shell", func(t *testing.T) {
		f := newTerminalFixture(t)
		expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/terminal", nil), http.StatusNoContent, "")
		if got := auditActions(t, f.server, "terminal."); len(got) != 0 {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("and reports why a terminal can't open", func(t *testing.T) {
		f := newTerminalFixture(t)
		f.server.clock.Advance(16 * time.Minute)
		expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/terminal", nil), http.StatusForbidden, "step_up_required")
	})
}

func TestTerminalRefusals(t *testing.T) {
	t.Run("viewers can't open a terminal", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "vic", "viewer")
		if _, status, err := f.dialTerminal(t, viewer, ""); err == nil || status != http.StatusForbidden {
			t.Errorf("dial = %v, want 403", err)
		}
	})

	t.Run("a machine whose policy disables the terminal is refused before connecting", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		events, stop := server.app.Hub.Subscribe()
		defer stop()
		token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
		agentID, _ := server.connectAgent(token.Secret, token.ServerFingerprint)
		waitForAgentEvent(t, events, agentID, hub.EventInfoUpdated)
		f := terminalFixture{server: server, admin: admin, agentID: agentID}
		if _, status, err := f.dialTerminal(t, admin.browser, ""); err == nil || status != http.StatusForbidden {
			t.Errorf("dial = %v, want 403", err)
		}
	})

	t.Run("an offline machine is refused", func(t *testing.T) {
		f := newTerminalFixture(t)
		events, stop := f.server.app.Hub.Subscribe()
		defer stop()
		_ = f.agent.Close(websocket.StatusNormalClosure, "")
		waitForAgentEvent(t, events, f.agentID, hub.EventOffline)
		if _, status, err := f.dialTerminal(t, f.admin.browser, ""); err == nil || status != http.StatusConflict {
			t.Errorf("dial = %v, want 409", err)
		}
	})

	t.Run("an unknown machine or session is not found", func(t *testing.T) {
		f := newTerminalFixture(t)
		if _, status, err := f.dialTerminal(t, f.admin.browser, "?resume=nope"); err == nil || status != http.StatusNotFound {
			t.Errorf("resume: %v", err)
		}
		f.agentID = "missing"
		if _, status, err := f.dialTerminal(t, f.admin.browser, ""); err == nil || status != http.StatusNotFound {
			t.Errorf("unknown machine: %v", err)
		}
	})
}

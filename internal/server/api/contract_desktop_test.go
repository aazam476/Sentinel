// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// newDesktopFixture is a signed-in admin and an online agent that reports
// desktop as given.
func newDesktopFixture(t *testing.T, desktop *protocol.DesktopAvailability, enabled bool) terminalFixture {
	t.Helper()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	events, stop := server.app.Hub.Subscribe()
	defer stop()
	token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
	agentID, agent := server.connectAgentWithInfo(token.Secret, token.ServerFingerprint, protocol.SystemInfo{
		Hostname: "nas-01", OS: "linux", Platform: "debian", Arch: "amd64", CPUCores: 4,
		Capabilities: []protocol.Capability{{Name: "desktop", Enabled: enabled}},
		Desktop:      desktop,
	})
	waitForAgentEvent(t, events, agentID, hub.EventInfoUpdated)
	return terminalFixture{server: server, admin: admin, agentID: agentID, agent: agent}
}

func availableDesktop() *protocol.DesktopAvailability {
	return &protocol.DesktopAvailability{Available: true}
}

// dialDesktop opens the desktop WebSocket as a browser would.
func (f terminalFixture) dialDesktop(t *testing.T, as *browser, query string) (*websocket.Conn, int, error) {
	t.Helper()
	url := "wss" + strings.TrimPrefix(f.server.server.URL, "https") + "/api/agents/" + f.agentID + "/desktop" + query
	conn, response, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{
		HTTPClient: as.client, HTTPHeader: http.Header{"Origin": {f.server.publicOrigin}},
	})
	status := 0
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

// acceptDesktop plays the agent's side of desktop.session.start.
func (f terminalFixture) acceptDesktop(t *testing.T) protocol.DesktopStartPayload {
	t.Helper()
	request := f.agentRead(t, protocol.TypeRequest)
	if request.Action != protocol.ActionDesktopSessionStart {
		t.Fatalf("agent got %s", request.Action)
	}
	var start protocol.DesktopStartPayload
	if err := protocol.DecodePayload(request, &start); err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeResponse, ID: request.ID},
		protocol.DesktopStartedPayload{Source: "test pattern", Width: 1280, Height: 720}); err != nil {
		t.Fatal(err)
	}
	return start
}

// agentFrame reads the next stream frame the agent gets.
func (f terminalFixture) agentFrame(t *testing.T) []byte {
	t.Helper()
	for {
		_, frame, err := protocol.ReadAny(t.Context(), f.agent)
		if err != nil {
			t.Fatal(err)
		}
		if frame != nil {
			_, data, err := protocol.DecodeFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
}

func TestDesktop(t *testing.T) {
	t.Run("relays a session's signaling and screen, and audits its start and end", func(t *testing.T) {
		f := newDesktopFixture(t, availableDesktop(), true)
		var details api.AgentDetails
		got := f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID, nil)
		if err := json.Unmarshal(got.body, &details); err != nil || !details.DesktopAvailable {
			t.Fatalf("details = %s (%v)", got.body, err)
		}
		browser, _, err := f.dialDesktop(t, f.admin.browser, "")
		if err != nil {
			t.Fatal(err)
		}
		start := f.acceptDesktop(t)
		if start.ViewOnly {
			t.Error("asked for a view-only session")
		}
		kind, data, err := browser.Read(t.Context())
		var ready api.DesktopReady
		if err != nil || kind != websocket.MessageText || json.Unmarshal(data, &ready) != nil {
			t.Fatalf("ready = %s (%v)", data, err)
		}
		if ready.Type != "ready" || ready.Source != "test pattern" || ready.Width != 1280 || ready.Height != 720 {
			t.Errorf("ready = %+v", ready)
		}
		// The agent and the browser get the same relay, with credentials
		// made for this session.
		if len(start.ICEServers) != 1 || !strings.HasPrefix(start.ICEServers[0].URLs[0], "turn:127.0.0.1:") ||
			start.ICEServers[0].Username == "" || start.ICEServers[0].Credential == "" {
			t.Errorf("agent's ICE servers = %+v", start.ICEServers)
		}
		if len(ready.ICEServers) != 1 || ready.ICEServers[0].Username != start.ICEServers[0].Username {
			t.Errorf("browser's ICE servers = %+v", ready.ICEServers)
		}

		// The browser's text goes to the agent as control messages; binary
		// is ignored.
		if err := browser.Write(t.Context(), websocket.MessageBinary, []byte{1, 2}); err != nil {
			t.Fatal(err)
		}
		if err := browser.Write(t.Context(), websocket.MessageText, []byte(`{"type":"offer","sdp":"v=0"}`)); err != nil {
			t.Fatal(err)
		}
		if data := f.agentFrame(t); string(data) != `c{"type":"offer","sdp":"v=0"}` {
			t.Errorf("agent got %q", data)
		}

		// The agent's control messages come back as text, its screen as
		// binary; anything else is dropped.
		for _, data := range [][]byte{{}, []byte("xjunk"), []byte(`c{"type":"answer"}`), {protocol.DesktopFrame, protocol.FrameEnd}} {
			if err := protocol.WriteFrame(t.Context(), f.agent, start.StreamID, data); err != nil {
				t.Fatal(err)
			}
		}
		if kind, data, err := browser.Read(t.Context()); err != nil || kind != websocket.MessageText || string(data) != `{"type":"answer"}` {
			t.Errorf("browser got %v %q (%v)", kind, data, err)
		}
		if kind, data, err := browser.Read(t.Context()); err != nil || kind != websocket.MessageBinary || !slices.Equal(data, []byte{protocol.FrameEnd}) {
			t.Errorf("browser got %v %v (%v)", kind, data, err)
		}

		if err := browser.Close(websocket.StatusNormalClosure, ""); err != nil {
			t.Fatal(err)
		}
		f.agentRead(t, protocol.TypeStreamClose)
		want := []string{"desktop.open:started", "desktop.close:success"}
		if got := eventualAuditActions(t, f.server, "desktop.", want); !slices.Equal(got, want) {
			t.Errorf("audit = %v, want %v", got, want)
		}
	})

	t.Run("asks the agent for a view-only session", func(t *testing.T) {
		f := newDesktopFixture(t, availableDesktop(), true)
		if _, _, err := f.dialDesktop(t, f.admin.browser, "?viewOnly=true"); err != nil {
			t.Fatal(err)
		}
		if start := f.acceptDesktop(t); !start.ViewOnly {
			t.Error("the session isn't view only")
		}
	})

	t.Run("ends the session when the agent closes the stream", func(t *testing.T) {
		f := newDesktopFixture(t, availableDesktop(), true)
		browser, _, err := f.dialDesktop(t, f.admin.browser, "")
		if err != nil {
			t.Fatal(err)
		}
		start := f.acceptDesktop(t)
		if _, _, err := browser.Read(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeStreamClose},
			protocol.StreamClosePayload{StreamID: start.StreamID, Reason: "the screen locked"}); err != nil {
			t.Fatal(err)
		}
		var ended error
		for ended == nil {
			_, _, ended = browser.Read(t.Context())
		}
		if websocket.CloseStatus(ended) != websocket.StatusNormalClosure || !strings.Contains(ended.Error(), "The screen locked.") {
			t.Errorf("browser got %v", ended)
		}
	})

	t.Run("the agent's refusal is audited and shown to the browser", func(t *testing.T) {
		f := newDesktopFixture(t, availableDesktop(), true)
		browser, _, err := f.dialDesktop(t, f.admin.browser, "")
		if err != nil {
			t.Fatal(err)
		}
		request := f.agentRead(t, protocol.TypeRequest)
		_ = protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeResponse, ID: request.ID,
			Error: &protocol.Error{Code: protocol.ErrorUnsupportedPlatform, Message: "Remote desktop isn't available: no one is logged in."}}, nil)
		_, _, err = browser.Read(t.Context())
		if websocket.CloseStatus(err) != websocket.StatusPolicyViolation || !strings.Contains(err.Error(), "no one is logged in") {
			t.Errorf("browser got %v", err)
		}
		if got := auditActions(t, f.server, "desktop."); len(got) != 1 || !strings.HasPrefix(got[0], "desktop.open:") {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestSEC07_OpeningADesktopNeedsARecentSecondFactor(t *testing.T) {
	f := newDesktopFixture(t, availableDesktop(), true)
	f.server.clock.Advance(16 * time.Minute)
	expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/desktop", nil), http.StatusForbidden, "step_up_required")
	expect(t, f.admin.browser.do(http.MethodPost, "/api/auth/step-up", api.TOTPCodeRequest{TOTPCode: f.server.code(f.admin.totpSecret)}), http.StatusOK, "")
	expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/desktop", nil), http.StatusNoContent, "")
}

func TestDesktopRefusals(t *testing.T) {
	tests := []struct {
		name    string
		desktop *protocol.DesktopAvailability
		enabled bool
		status  int
		code    string
		reason  string
	}{
		{"a machine whose policy disables the desktop is refused", availableDesktop(), false, http.StatusForbidden, "policy_denied", "local policy"},
		{"a machine with no desktop to share says why", &protocol.DesktopAvailability{Reason: "No one is logged in to a graphical session."}, true,
			http.StatusConflict, "desktop_unavailable", "No one is logged in"},
		{"an agent too old to say is asked to update", nil, true, http.StatusConflict, "desktop_unavailable", "too old"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newDesktopFixture(t, test.desktop, test.enabled)
			got := f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/desktop", nil)
			expect(t, got, test.status, test.code)
			if !strings.Contains(string(got.body), test.reason) {
				t.Errorf("body = %s, want %q", got.body, test.reason)
			}
		})
	}

	t.Run("viewers can't open a desktop", func(t *testing.T) {
		f := newDesktopFixture(t, availableDesktop(), true)
		viewer := f.server.addUser(f.admin, "vic", "viewer")
		if _, status, err := f.dialDesktop(t, viewer, ""); err == nil || status != http.StatusForbidden {
			t.Errorf("dial = %v, want 403", err)
		}
	})

	t.Run("an offline machine is refused", func(t *testing.T) {
		f := newDesktopFixture(t, availableDesktop(), true)
		events, stop := f.server.app.Hub.Subscribe()
		defer stop()
		_ = f.agent.Close(websocket.StatusNormalClosure, "")
		waitForAgentEvent(t, events, f.agentID, hub.EventOffline)
		expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/desktop", nil), http.StatusConflict, "machine_offline")
	})

	t.Run("an unknown machine is not found", func(t *testing.T) {
		f := newDesktopFixture(t, availableDesktop(), true)
		expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/missing/desktop", nil), http.StatusNotFound, "not_found")
	})
}

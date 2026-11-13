// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
)

// createToken has the admin create an enrollment token.
func createToken(t *testing.T, admin admin, request api.CreateEnrollmentTokenRequest) api.CreateEnrollmentTokenResponse {
	t.Helper()
	created := admin.browser.do(http.MethodPost, "/api/enrollment-tokens", request)
	expect(t, created, http.StatusCreated, "")
	var response api.CreateEnrollmentTokenResponse
	created.decode(t, &response)
	return response
}

// connectAgent enrolls an agent with the token over /agent/connect, runs
// the handshake and answers the server's system.info request. It returns
// the agent's ID and its open connection.
func (s *testServer) connectAgent(secret, fingerprint string) (string, *websocket.Conn) {
	s.t.Helper()
	return s.connectAgentWith(secret, fingerprint, []protocol.Capability{{Name: "terminal", Enabled: false, Reason: "Disabled by local policy"}})
}

// connectAgentWith is connectAgent with the capabilities the agent reports.
func (s *testServer) connectAgentWith(secret, fingerprint string, capabilities []protocol.Capability) (string, *websocket.Conn) {
	s.t.Helper()
	return s.connectAgentWithInfo(secret, fingerprint, protocol.SystemInfo{Hostname: "nas-01", OS: "linux", Platform: "debian", Arch: "amd64", CPUCores: 4,
		Capabilities: capabilities})
}

// connectAgentWithInfo is connectAgent with all the agent reports.
func (s *testServer) connectAgentWithInfo(secret, fingerprint string, info protocol.SystemInfo) (string, *websocket.Conn) {
	s.t.Helper()
	url := "wss" + strings.TrimPrefix(s.server.URL, "https") + "/agent/connect"
	conn, _, err := websocket.Dial(s.t.Context(), url, &websocket.DialOptions{HTTPClient: s.server.Client()}) //nolint:bodyclose // closed by coder/websocket
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { _ = conn.CloseNow() }) // may already be closed
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		s.t.Fatal(err)
	}
	s.write(conn, protocol.TypeEnroll, protocol.EnrollPayload{
		Token: secret, PublicKey: public, Hostname: "nas-01", OS: "linux", Arch: "amd64",
		AgentVersion: "v0.1.0", ProtocolVersion: protocol.CurrentVersion,
	})
	var enrolled protocol.EnrolledPayload
	s.read(conn, protocol.TypeEnrolled, &enrolled)
	if !protocol.FingerprintMatches(fingerprint, enrolled.ServerPublicKey) {
		s.t.Fatal("the server's key doesn't match the fingerprint the API gave out")
	}
	nonce := protocol.NewNonce()
	s.write(conn, protocol.TypeHello, protocol.HelloPayload{
		AgentID: enrolled.AgentID, AgentVersion: "v0.1.0", ProtocolVersion: protocol.CurrentVersion, AgentNonce: nonce,
	})
	var challenge protocol.ChallengePayload
	s.read(conn, protocol.TypeChallenge, &challenge)
	s.write(conn, protocol.TypeProve, protocol.ProvePayload{
		Signature: protocol.SignAgentProof(private, challenge.ServerNonce, nonce, enrolled.AgentID),
	})
	s.read(conn, protocol.TypeWelcome, &protocol.WelcomePayload{})
	request, err := protocol.ReadMessage(s.t.Context(), conn)
	if err != nil {
		s.t.Fatal(err)
	}
	if err := protocol.WriteMessage(s.t.Context(), conn, protocol.Message{Type: protocol.TypeResponse, ID: request.ID}, info); err != nil {
		s.t.Fatal(err)
	}
	return enrolled.AgentID, conn
}

func (s *testServer) write(conn *websocket.Conn, kind protocol.MessageType, payload any) {
	s.t.Helper()
	if err := protocol.WriteMessage(s.t.Context(), conn, protocol.Message{Type: kind}, payload); err != nil {
		s.t.Fatal(err)
	}
}

func (s *testServer) read(conn *websocket.Conn, kind protocol.MessageType, destination any) {
	s.t.Helper()
	if err := protocol.ReadPayload(s.t.Context(), conn, kind, destination); err != nil {
		s.t.Fatal(err)
	}
}

// waitForAgentEvent waits for the hub to announce kind for an agent.
func waitForAgentEvent(t *testing.T, events <-chan hub.Event, agentID string, kind hub.EventKind) {
	t.Helper()
	for {
		select {
		case event := <-events:
			if event.PublicID == agentID && event.Kind == kind {
				return
			}
		case <-t.Context().Done():
			t.Fatalf("no %s event for %s", kind, agentID)
		}
	}
}

func TestEnrollmentTokensAPI(t *testing.T) {
	t.Run("an admin creates, lists and revokes tokens; the secret is shown once", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		created := createToken(t, admin, api.CreateEnrollmentTokenRequest{Note: "office NAS", ExpiresInHours: 48, MaxUses: 3})
		if created.Secret == "" || !strings.HasPrefix(created.ServerFingerprint, "SHA256:") || !created.Token.Usable ||
			created.Token.ExpiresAt-created.Token.CreatedAt != (48*time.Hour).Milliseconds() || created.Token.CreatedBy != "admin" {
			t.Errorf("created = %+v", created)
		}

		listed := admin.browser.do(http.MethodGet, "/api/enrollment-tokens", nil)
		expect(t, listed, http.StatusOK, "")
		if strings.Contains(string(listed.body), created.Secret) {
			t.Error("the list shows the token's secret")
		}
		var tokens api.EnrollmentTokensResponse
		listed.decode(t, &tokens)
		if len(tokens.Tokens) != 1 || tokens.ServerFingerprint != created.ServerFingerprint {
			t.Errorf("tokens = %+v", tokens)
		}

		expect(t, admin.browser.do(http.MethodDelete, fmt.Sprintf("/api/enrollment-tokens/%d", created.Token.ID), nil), http.StatusNoContent, "")
		admin.browser.do(http.MethodGet, "/api/enrollment-tokens", nil).decode(t, &tokens)
		if !tokens.Tokens[0].Revoked || tokens.Tokens[0].Usable {
			t.Errorf("after revoking: %+v", tokens.Tokens[0])
		}
	})

	t.Run("refuses an invalid token request with the field at fault", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		got := admin.browser.do(http.MethodPost, "/api/enrollment-tokens", api.CreateEnrollmentTokenRequest{ExpiresInHours: 24 * 8})
		expect(t, got, http.StatusBadRequest, "invalid_input")
		if !strings.Contains(string(got.body), "expiresInHours") {
			t.Errorf("body = %s, want the field", got.body)
		}
		expect(t, admin.browser.do(http.MethodDelete, "/api/enrollment-tokens/99", nil), http.StatusNotFound, "not_found")
		expect(t, admin.browser.do(http.MethodDelete, "/api/enrollment-tokens/x", nil), http.StatusNotFound, "not_found")
	})

	t.Run("operators and viewers can't touch tokens", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		operator := server.addUser(admin, "olu", "operator")
		expect(t, operator.do(http.MethodGet, "/api/enrollment-tokens", nil), http.StatusForbidden, "forbidden")
		expect(t, operator.do(http.MethodPost, "/api/enrollment-tokens", api.CreateEnrollmentTokenRequest{}), http.StatusForbidden, "forbidden")
	})
}

func TestSEC21_AnAgentEnrollsThroughTheServerAndRevocationDisconnectsIt(t *testing.T) {
	server := newTestServer(t)
	admin := server.setUpAdmin()
	events, stop := server.app.Hub.Subscribe()
	defer stop()
	token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
	agentID, conn := server.connectAgent(token.Secret, token.ServerFingerprint)
	waitForAgentEvent(t, events, agentID, hub.EventInfoUpdated)

	var agents api.AgentsResponse
	admin.browser.do(http.MethodGet, "/api/agents", nil).decode(t, &agents)
	if len(agents.Agents) != 1 || agents.Agents[0].ID != agentID || !agents.Agents[0].Online || agents.Agents[0].Platform != "debian" {
		t.Fatalf("agents = %+v", agents)
	}

	expect(t, admin.browser.do(http.MethodPost, "/api/agents/"+agentID+"/revoke", nil), http.StatusNoContent, "")
	_, err := protocol.ReadMessage(t.Context(), conn)
	if got := websocket.CloseStatus(err); got != protocol.CloseRevoked {
		t.Errorf("close status = %d, want CloseRevoked", got)
	}
	admin.browser.do(http.MethodGet, "/api/agents", nil).decode(t, &agents)
	if len(agents.Agents) != 0 {
		t.Errorf("a revoked machine is still listed: %+v", agents.Agents)
	}
}

func TestAgentsAPI(t *testing.T) {
	t.Run("shows a machine's details, capabilities and current metrics", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		events, stop := server.app.Hub.Subscribe()
		defer stop()
		token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
		agentID, conn := server.connectAgent(token.Secret, token.ServerFingerprint)
		waitForAgentEvent(t, events, agentID, hub.EventInfoUpdated)
		sample := protocol.MetricsSample{Time: server.clock.Now().UnixMilli(), CPUPercent: 37, MemoryUsedBytes: 1, MemoryTotalBytes: 4,
			Filesystems: []protocol.FilesystemUsage{{Mount: "/", Type: "ext4", UsedBytes: 5, TotalBytes: 10}}, UptimeSeconds: 99}
		if err := protocol.WriteMessage(t.Context(), conn, protocol.Message{Type: protocol.TypeEvent,
			ID: protocol.NewID(server.clock.Now()), Action: protocol.ActionMetricsSample}, sample); err != nil {
			t.Fatal(err)
		}
		waitForAgentEvent(t, events, agentID, hub.EventMetrics)

		got := admin.browser.do(http.MethodGet, "/api/agents/"+agentID, nil)
		expect(t, got, http.StatusOK, "")
		var details api.AgentDetails
		got.decode(t, &details)
		if details.CPUCores != 4 || len(details.Capabilities) != 1 || details.Capabilities[0].Enabled ||
			details.Current == nil || details.Current.Values[metrics.CPUPercent] != 37 || details.Current.UptimeSeconds != 99 ||
			len(details.Filesystems) != 1 {
			t.Errorf("details = %+v", details)
		}

		series := admin.browser.do(http.MethodGet, fmt.Sprintf("/api/agents/%s/metrics?metric=%s&from=%d&to=%d",
			agentID, metrics.DiskPercent, sample.Time-1000, sample.Time+1000), nil)
		expect(t, series, http.StatusOK, "")
		var points api.MetricSeries
		series.decode(t, &points)
		if points.Resolution != "raw" || len(points.Points) != 1 || points.Points[0].Value != 50 {
			t.Errorf("series = %+v", points)
		}
	})

	t.Run("an admin renames and tags a machine; a viewer can only look", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		viewer := server.addUser(admin, "vic", "viewer")
		token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
		agentID, _ := server.connectAgent(token.Secret, token.ServerFingerprint)

		name, tags := "Living room NAS", []string{"homelab", "Storage"}
		updated := admin.browser.do(http.MethodPatch, "/api/agents/"+agentID, api.UpdateAgentRequest{DisplayName: &name, Tags: &tags})
		expect(t, updated, http.StatusOK, "")
		var details api.AgentDetails
		updated.decode(t, &details)
		if details.Name != name || !slices.Equal(details.Tags, []string{"homelab", "storage"}) {
			t.Errorf("details = %+v", details)
		}
		expect(t, viewer.do(http.MethodGet, "/api/agents/"+agentID, nil), http.StatusOK, "")
		expect(t, viewer.do(http.MethodPatch, "/api/agents/"+agentID, api.UpdateAgentRequest{}), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodPost, "/api/agents/"+agentID+"/revoke", nil), http.StatusForbidden, "forbidden")
		bad := []string{"has space"}
		expect(t, admin.browser.do(http.MethodPatch, "/api/agents/"+agentID, api.UpdateAgentRequest{Tags: &bad}), http.StatusBadRequest, "invalid_input")
	})

	t.Run("reports unknown machines and bad metric queries", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
		agentID, _ := server.connectAgent(token.Secret, token.ServerFingerprint)
		expect(t, admin.browser.do(http.MethodGet, "/api/agents/nope", nil), http.StatusNotFound, "not_found")
		expect(t, admin.browser.do(http.MethodPatch, "/api/agents/nope", api.UpdateAgentRequest{}), http.StatusNotFound, "not_found")
		expect(t, admin.browser.do(http.MethodPost, "/api/agents/nope/revoke", nil), http.StatusNotFound, "not_found")
		expect(t, admin.browser.do(http.MethodPatch, "/api/agents/"+agentID, "not an object"), http.StatusBadRequest, "invalid_json")
		queries := []string{
			"metric=cpu.percent&from=x&to=1",
			"metric=cpu.percent&from=0&to=-1",
			fmt.Sprintf("metric=cpu.percent&from=0&to=%d", (6 * 365 * 24 * time.Hour).Milliseconds()), // beyond five years of hourly history
			"metric=nonsense&from=0&to=1000",
		}
		for _, query := range queries {
			expect(t, admin.browser.do(http.MethodGet, "/api/agents/"+agentID+"/metrics?"+query, nil), http.StatusBadRequest, "invalid_input")
		}
		expect(t, admin.browser.do(http.MethodGet, "/api/agents/nope/metrics?metric=cpu.percent&from=0&to=1", nil), http.StatusNotFound, "not_found")
	})
}

func TestEventsAPI(t *testing.T) {
	t.Run("streams machines coming online and their metrics to signed-in users", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.server.URL+"/api/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := admin.browser.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }() // the stream is cancelled; nothing to report
		if response.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("content type = %q", response.Header.Get("Content-Type"))
		}
		lines := bufio.NewScanner(response.Body)
		lines.Scan() // ": connected", sent once the subscription exists

		token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
		agentID, conn := server.connectAgent(token.Secret, token.ServerFingerprint)
		if err := protocol.WriteMessage(t.Context(), conn, protocol.Message{Type: protocol.TypeEvent,
			ID: protocol.NewID(server.clock.Now()), Action: protocol.ActionMetricsSample},
			protocol.MetricsSample{Time: server.clock.Now().UnixMilli(), CPUPercent: 12}); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"event: agent", `data: {"agentId":"` + agentID + `","kind":"online"}`,
			"event: metrics", `data: {"agentId":"` + agentID + `","current":`,
		}
		for len(want) > 0 && lines.Scan() {
			if strings.HasPrefix(lines.Text(), want[0]) {
				want = want[1:]
			}
		}
		if len(want) > 0 {
			t.Errorf("stream ended before %q", want[0])
		}
	})

	t.Run("needs a signed-in user", func(t *testing.T) {
		server := newTestServer(t)
		expect(t, server.newBrowser().do(http.MethodGet, "/api/events", nil), http.StatusUnauthorized, "not_authenticated")
	})

	t.Run("ends when the session is revoked", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		other := server.newBrowser()
		expect(t, other.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "admin", Password: strongPassword}), http.StatusOK, "")
		expect(t, other.do(http.MethodPost, "/api/auth/login/second-factor", api.SecondFactorRequest{TOTPCode: server.code(admin.totpSecret)}), http.StatusOK, "")

		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.server.URL+"/api/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := other.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }() // nothing to report
		lines := bufio.NewScanner(response.Body)
		lines.Scan()

		expect(t, other.do(http.MethodPost, "/api/auth/logout", nil), http.StatusNoContent, "")
		if err := server.clock.WaitForTimers(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		server.clock.Advance(30 * time.Second)
		for lines.Scan() { // the server ends the stream
		}
	})

	t.Run("ends when the server shuts down", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.server.URL+"/api/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := admin.browser.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }() // nothing to report
		lines := bufio.NewScanner(response.Body)
		lines.Scan()
		server.app.Hub.Shutdown()
		for lines.Scan() {
		}
	})
}

func TestSEC13_BrowsersCantOpenTheAgentEndpoint(t *testing.T) {
	server := newTestServer(t)
	url := "wss" + strings.TrimPrefix(server.server.URL, "https") + "/agent/connect"
	for _, origin := range []string{server.publicOrigin, "https://evil.example"} {
		_, response, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{
			HTTPClient: server.server.Client(), HTTPHeader: http.Header{"Origin": {origin}},
		})
		if err == nil {
			t.Fatalf("a page at %s opened the agent endpoint", origin)
		}
		_ = response.Body.Close() // nothing to read
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("origin %s: status %d, want 403", origin, response.StatusCode)
		}
	}
}

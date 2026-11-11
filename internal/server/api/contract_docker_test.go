// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/audit"
)

func dockerPath(f terminalFixture, rest string) string {
	return "/api/agents/" + f.agentID + "/docker" + rest
}

func TestDockerContainers(t *testing.T) {
	t.Run("lists containers for viewers, and acts on them for operators with an audit entry", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		answerNext(t, f.agent, protocol.ContainerListPayload{Containers: []protocol.Container{{ID: "abc", Name: "web", State: "running"}}}, nil)
		got := viewer.do(http.MethodGet, dockerPath(f, "/containers"), nil)
		expect(t, got, http.StatusOK, "")
		var list protocol.ContainerListPayload
		got.decode(t, &list)
		if len(list.Containers) != 1 || list.Containers[0].Name != "web" {
			t.Errorf("list = %+v", list)
		}
		expect(t, viewer.do(http.MethodPost, dockerPath(f, "/containers/web/restart"), nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodGet, dockerPath(f, "/containers/web/inspect"), nil), http.StatusForbidden, "forbidden")

		asked := answerNext(t, f.agent, nil, nil)
		expect(t, f.admin.browser.do(http.MethodPost, dockerPath(f, "/containers/web/restart"), nil), http.StatusNoContent, "")
		if request := <-asked; request.action != protocol.ActionDockerContainerRestart || request.payload != `{"id":"web"}` {
			t.Errorf("agent got %+v", request)
		}
		asked = answerNext(t, f.agent, nil, &protocol.Error{Code: protocol.ErrorConflict, Message: "container is running"})
		expect(t, f.admin.browser.do(http.MethodDelete, dockerPath(f, "/containers/web"), nil), http.StatusConflict, "conflict")
		if request := <-asked; request.payload != `{"id":"web"}` {
			t.Errorf("agent got %+v", request)
		}
		want := []string{"docker.container.restart:success", "docker.container.remove:failure"}
		if got := auditActions(t, f.server, "docker."); !slices.Equal(got, want) {
			t.Errorf("audit = %v", got)
		}
		expect(t, f.admin.browser.do(http.MethodPost, dockerPath(f, "/containers/web/pause"), nil), http.StatusNotFound, "not_found")
	})

	t.Run("reads recent logs, and refuses odd limits", func(t *testing.T) {
		f := newTerminalFixture(t)
		asked := answerNext(t, f.agent, protocol.ContainerLogsResultPayload{Lines: []protocol.ContainerLogLine{{Time: 1, Stream: "stderr", Text: "boom"}}}, nil)
		got := f.admin.browser.do(http.MethodGet, dockerPath(f, "/containers/web/logs?tail=100&since=5"), nil)
		expect(t, got, http.StatusOK, "")
		if request := <-asked; request.payload != `{"id":"web","tail":100,"since":5}` {
			t.Errorf("agent got %+v", request)
		}
		for _, query := range []string{"?tail=0", "?tail=x", "?since=-1"} {
			expect(t, f.admin.browser.do(http.MethodGet, dockerPath(f, "/containers/web/logs"+query), nil), http.StatusBadRequest, "invalid_input")
		}
	})

	t.Run("follows a log as server-sent events until the container stops", func(t *testing.T) {
		f := newTerminalFixture(t)
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.server.URL+dockerPath(f, "/containers/web/logs/follow"), nil)
		if err != nil {
			t.Fatal(err)
		}
		response := make(chan *http.Response, 1)
		go func() {
			got, err := f.admin.browser.client.Do(request) //nolint:bodyclose // closed below
			if err != nil {
				t.Error(err)
			}
			response <- got
		}()
		open := f.agentRead(t, protocol.TypeRequest)
		var payload protocol.ContainerLogsPayload
		if err := protocol.DecodePayload(open, &payload); err != nil || payload.StreamID == 0 {
			t.Fatalf("payload = %+v (%v)", payload, err)
		}
		recent := protocol.ContainerLogsResultPayload{Lines: []protocol.ContainerLogLine{{Time: 1, Stream: "stdout", Text: "old"}}}
		if err := protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeResponse, ID: open.ID}, recent); err != nil {
			t.Fatal(err)
		}
		if err := protocol.WriteFrame(t.Context(), f.agent, payload.StreamID, []byte(`{"time":2,"stream":"stderr","text":"new"}`+"\n")); err != nil {
			t.Fatal(err)
		}
		if err := protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeStreamClose},
			protocol.StreamClosePayload{StreamID: payload.StreamID, Reason: "the container stopped"}); err != nil {
			t.Fatal(err)
		}
		got := <-response
		defer got.Body.Close() //nolint:errcheck // test
		var events []string
		scanner := bufio.NewScanner(got.Body)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "data:") {
				events = append(events, line)
			}
		}
		want := []string{
			"event: line", `data: {"time":1,"stream":"stdout","text":"old"}`,
			"event: line", `data: {"time":2,"stream":"stderr","text":"new"}`,
			"event: end", `data: {"reason":"the container stopped"}`,
		}
		if !slices.Equal(events, want) {
			t.Errorf("events = %q", events)
		}
	})

	t.Run("SEC-07: opens a shell in a container only after a recent second factor, and audits it", func(t *testing.T) {
		f := newTerminalFixture(t)
		url := "wss" + strings.TrimPrefix(f.server.server.URL, "https") + dockerPath(f, "/containers/web/exec")
		dial := func() (*websocket.Conn, error) {
			conn, response, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{
				HTTPClient: f.admin.browser.client, HTTPHeader: http.Header{"Origin": {f.server.publicOrigin}}})
			if response != nil && response.Body != nil {
				_ = response.Body.Close() // nothing to read
			}
			return conn, err
		}
		browser, err := dial()
		if err != nil {
			t.Fatal(err)
		}
		open := f.agentRead(t, protocol.TypeRequest)
		var payload protocol.ContainerExecPayload
		if err := protocol.DecodePayload(open, &payload); err != nil || open.Action != protocol.ActionDockerContainerExec || payload.ID != "web" || payload.User != "admin" {
			t.Fatalf("agent got %s %+v (%v)", open.Action, payload, err)
		}
		if err := protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeResponse, ID: open.ID},
			protocol.TerminalOpenedPayload{SessionID: "x1"}); err != nil {
			t.Fatal(err)
		}
		if status := browserStatus(t, browser); status.SessionID != "x1" {
			t.Errorf("status = %+v", status)
		}
		_ = browser.Close(websocket.StatusNormalClosure, "")
		for {
			if _, _, err := browser.Read(t.Context()); err != nil {
				break
			}
		}
		f.server.clock.Advance(16 * 60 * 1e9)
		expect(t, f.admin.browser.do(http.MethodGet, dockerPath(f, "/containers/web/exec"), nil), http.StatusForbidden, "step_up_required")
		if got := auditActions(t, f.server, "docker.exec."); len(got) == 0 || got[0] != "docker.exec.open:started" {
			t.Errorf("audit = %v", got)
		}
	})
}

func TestDockerImagesAndStacks(t *testing.T) {
	t.Run("SEC-42: pulls with the stored login for the image's registry, never showing it", func(t *testing.T) {
		f := newTerminalFixture(t)
		added := f.admin.browser.do(http.MethodPost, "/api/registries", api.AddRegistryLoginRequest{ServerAddress: "ghcr.io", Username: "bot", Password: "ghp_token"})
		expect(t, added, http.StatusCreated, "")
		if strings.Contains(string(added.body), "ghp_token") {
			t.Error("the password came back")
		}
		listed := f.admin.browser.do(http.MethodGet, "/api/registries", nil)
		expect(t, listed, http.StatusOK, "")
		if strings.Contains(string(listed.body), "ghp_token") {
			t.Error("the password is listed")
		}
		operator := f.server.addUser(f.admin, "olu", "operator")
		expect(t, operator.do(http.MethodGet, "/api/registries", nil), http.StatusForbidden, "forbidden")
		expect(t, f.admin.browser.do(http.MethodPost, "/api/registries", api.AddRegistryLoginRequest{ServerAddress: "ghcr.io", Username: "x", Password: "y"}),
			http.StatusConflict, "registry_exists")

		asked := answerNext(t, f.agent, protocol.ImagePulledPayload{Digest: "sha256:abc"}, nil)
		expect(t, f.admin.browser.do(http.MethodPost, dockerPath(f, "/images/pull"), api.ImageReferenceRequest{Reference: "ghcr.io/acme/app:2"}), http.StatusNoContent, "")
		var sent protocol.ImageRefPayload
		if err := json.Unmarshal([]byte((<-asked).payload), &sent); err != nil || sent.Auth == nil || sent.Auth.Password != "ghp_token" {
			t.Errorf("agent got %+v (%v)", sent, err)
		}
		records := auditActions(t, f.server, "")
		if !slices.Contains(records, "docker.image.pull:success") || !slices.Contains(records, "registry.add:success") {
			t.Errorf("audit = %v", records)
		}
		var logins api.RegistryLogins
		listed.decode(t, &logins)
		expect(t, f.admin.browser.do(http.MethodDelete, "/api/registries/"+itoa(logins.Logins[0].ID), nil), http.StatusNoContent, "")
		expect(t, f.admin.browser.do(http.MethodDelete, "/api/registries/"+itoa(logins.Logins[0].ID), nil), http.StatusNotFound, "not_found")
		expect(t, f.admin.browser.do(http.MethodDelete, "/api/registries/x", nil), http.StatusNotFound, "not_found")
	})

	t.Run("checks images for updates and shows the results with the list", func(t *testing.T) {
		f := newTerminalFixture(t)
		images := protocol.ImageListPayload{Images: []protocol.Image{{ID: "sha256:1", Tags: []string{"nginx:1.27"}}}}
		answerInOrder(t, f.agent, images, protocol.ImageCheckPayload{RegistryDigest: "sha256:new", UpdateAvailable: true}, images)
		got := f.admin.browser.do(http.MethodPost, dockerPath(f, "/images/check"), nil)
		expect(t, got, http.StatusOK, "")
		var result api.DockerImages
		got.decode(t, &result)
		if len(result.Images) != 1 || !result.Updates["nginx:1.27"].UpdateAvailable {
			t.Errorf("images = %+v", result)
		}
		answerNext(t, f.agent, images, nil)
		listed := f.admin.browser.do(http.MethodGet, dockerPath(f, "/images"), nil)
		expect(t, listed, http.StatusOK, "")
		listed.decode(t, &result)
		if !result.Updates["nginx:1.27"].UpdateAvailable {
			t.Errorf("listed = %+v", result)
		}
	})

	t.Run("saves, checks and deploys stacks, auditing changes but not their files", func(t *testing.T) {
		f := newTerminalFixture(t)
		asked := answerNext(t, f.agent, nil, nil)
		expect(t, f.admin.browser.do(http.MethodPut, dockerPath(f, "/stacks/blog"), api.SaveStackRequest{Compose: "services: {}", Env: "PASSWORD=secret", ValidateOnly: true}),
			http.StatusNoContent, "")
		<-asked
		asked = answerNext(t, f.agent, nil, nil)
		expect(t, f.admin.browser.do(http.MethodPut, dockerPath(f, "/stacks/blog"), api.SaveStackRequest{Compose: "services: {}", Env: "PASSWORD=secret"}),
			http.StatusNoContent, "")
		<-asked
		asked = answerNext(t, f.agent, nil, nil)
		expect(t, f.admin.browser.do(http.MethodPost, dockerPath(f, "/stacks/blog/up"), nil), http.StatusNoContent, "")
		if request := <-asked; request.action != protocol.ActionDockerStackUp {
			t.Errorf("agent got %+v", request)
		}
		expect(t, f.admin.browser.do(http.MethodPost, dockerPath(f, "/stacks/blog/explode"), nil), http.StatusNotFound, "not_found")
		want := []string{"docker.stack.save:success", "docker.stack.up:success"}
		if got := auditActions(t, f.server, "docker.stack."); !slices.Equal(got, want) {
			t.Errorf("audit = %v", got)
		}
		records, err := f.server.app.AuditLog.List(t.Context(), auditListAll())
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range records {
			if strings.Contains(string(record.Details), "secret") {
				t.Errorf("%s holds the stack's secrets: %s", record.Action, record.Details)
			}
		}
		asked = answerNext(t, f.agent, protocol.StackFilesPayload{Name: "blog", Env: "PASSWORD=secret"}, nil)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, viewer.do(http.MethodGet, dockerPath(f, "/stacks/blog"), nil), http.StatusForbidden, "forbidden")
		expect(t, f.admin.browser.do(http.MethodGet, dockerPath(f, "/stacks/blog"), nil), http.StatusOK, "")
		<-asked
	})

	t.Run("removes volumes, networks and images, and lists them", func(t *testing.T) {
		f := newTerminalFixture(t)
		for _, c := range []struct{ method, path, action string }{
			{http.MethodDelete, "/volumes/data", protocol.ActionDockerVolumeRemove},
			{http.MethodDelete, "/networks/net", protocol.ActionDockerNetworkRemove},
			{http.MethodDelete, "/images?reference=nginx:1.27&force=true", protocol.ActionDockerImageRemove},
		} {
			asked := answerNext(t, f.agent, nil, nil)
			expect(t, f.admin.browser.do(c.method, dockerPath(f, c.path), nil), http.StatusNoContent, "")
			if request := <-asked; request.action != c.action {
				t.Errorf("%s: agent got %+v", c.path, request)
			}
		}
		for _, path := range []string{"/volumes", "/networks", "/stacks"} {
			answerNext(t, f.agent, map[string]any{}, nil)
			expect(t, f.admin.browser.do(http.MethodGet, dockerPath(f, path), nil), http.StatusOK, "")
		}
		answerNext(t, f.agent, nil, &protocol.Error{Code: protocol.ErrorDockerUnavailable, Message: "docker not detected"})
		expect(t, f.admin.browser.do(http.MethodGet, dockerPath(f, "/containers"), nil), http.StatusServiceUnavailable, "docker_unavailable")
	})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func auditListAll() audit.ListOptions { return audit.ListOptions{Limit: 1000} }

// answerInOrder plays the agent for several requests in a row, from one
// goroutine (two readers on one WebSocket would split its frames).
func answerInOrder(t *testing.T, conn *websocket.Conn, results ...any) {
	t.Helper()
	go func() {
		for _, result := range results {
			for {
				message, frame, err := protocol.ReadAny(t.Context(), conn)
				if err != nil {
					t.Errorf("the agent's read failed: %v", err)
					return
				}
				if frame == nil && message.Type == protocol.TypeRequest {
					if err := protocol.WriteMessage(t.Context(), conn, protocol.Message{Type: protocol.TypeResponse, ID: message.ID}, result); err != nil {
						t.Errorf("the agent's answer failed: %v", err)
					}
					break
				}
			}
		}
	}()
}

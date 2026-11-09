// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/shared/release"
)

// signedBuild is an agent build signed with the development release key,
// which test builds trust.
func signedBuild(t *testing.T, version string, content []byte) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	text, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "release", "development.key"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := release.ParsePrivateKey(string(text))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := release.ManifestFor(bytes.NewReader(content), "sentinel-agent", version, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := release.Sign(manifest, key)
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

// upload sends a build and its signature as the Updates page does.
func upload(t *testing.T, as *browser, content, signature []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, data := range map[string][]byte{"signature": signature} {
		part, _ := form.CreateFormFile(name, name)
		_, _ = part.Write(data)
	}
	part, _ := form.CreateFormFile("build", "sentinel-agent")
	_, _ = part.Write(content)
	_ = form.Close()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, as.server.server.URL+"/api/agent-builds", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("Origin", as.server.publicOrigin)
	return as.send(request)
}

func TestAgentUpdatesAPI(t *testing.T) {
	t.Run("keeps viewers away from builds and updates", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, viewer.do(http.MethodGet, "/api/agent-builds", nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodPost, "/api/agent-updates", api.UpdateAgentsRequest{Target: api.ScriptTarget{Kind: "all"}}), http.StatusForbidden, "forbidden")
	})

	t.Run("hosts an uploaded build, serves it to anyone, and updates a machine with it", func(t *testing.T) {
		f := newTerminalFixture(t)
		content := []byte("\x7fELF agent v0.2.0")
		created := upload(t, f.admin.browser, content, signedBuild(t, "v0.2.0", content))
		expect(t, created, http.StatusCreated, "")
		var build api.AgentBuild
		created.decode(t, &build)
		if build.Version != "v0.2.0" || build.File != "sentinel-agent_v0.2.0_linux_amd64" {
			t.Errorf("build = %+v", build)
		}
		var builds api.AgentBuilds
		f.admin.browser.do(http.MethodGet, "/api/agent-builds", nil).decode(t, &builds)
		if len(builds.Builds) != 1 || !builds.CanFetch || builds.AutoUpdate || builds.DevelopmentKey != release.IsDevelopmentBuild() {
			t.Errorf("builds = %+v", builds)
		}
		served := f.server.newBrowser().do(http.MethodGet, "/install/updates/v0.2.0/sentinel-agent_v0.2.0_linux_amd64", nil)
		if served.status != http.StatusOK || !bytes.Equal(served.body, content) {
			t.Errorf("served %d %q", served.status, served.body)
		}

		asked := answerNext(t, f.agent, struct{}{}, nil)
		var outcome api.UpdateAgentsResponse
		started := f.admin.browser.do(http.MethodPost, "/api/agent-updates", api.UpdateAgentsRequest{Target: api.ScriptTarget{Kind: "agent", Value: f.agentID}})
		expect(t, started, http.StatusOK, "")
		started.decode(t, &outcome)
		if len(outcome.Started) != 1 {
			t.Fatalf("outcome = %+v", outcome)
		}
		request := <-asked
		var payload protocol.AgentUpdatePayload
		_ = json.Unmarshal([]byte(request.payload), &payload)
		if request.action != protocol.ActionAgentUpdate || payload.Version != "v0.2.0" || payload.Path != "/install/updates/v0.2.0/sentinel-agent_v0.2.0_linux_amd64" {
			t.Errorf("asked %+v", request)
		}
		var details api.AgentDetails
		f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID, nil).decode(t, &details)
		if details.Update == nil || details.Update.State != "updating" || details.Update.Version != "v0.2.0" {
			t.Errorf("update = %+v", details.Update)
		}

		// The machine reports its update rolled back.
		events, stop := f.server.app.Hub.Subscribe()
		defer stop()
		result := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(f.server.clock.Now()), Action: protocol.ActionAgentUpdateResult}
		if err := protocol.WriteMessage(t.Context(), f.agent, result, protocol.AgentUpdateResultPayload{Version: "v0.2.0", RolledBack: true, Error: "didn't connect"}); err != nil {
			t.Fatal(err)
		}
		waitForAgentEvent(t, events, f.agentID, hub.EventInfoUpdated)
		f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID, nil).decode(t, &details)
		if details.Update == nil || details.Update.State != "rolled_back" || details.Update.Error != "didn't connect" || details.Update.FinishedAt == nil {
			t.Errorf("update = %+v", details.Update)
		}
	})

	t.Run("refuses a build whose signature doesn't check out, and an upload that isn't a form", func(t *testing.T) {
		f := newTerminalFixture(t)
		content := []byte("agent")
		expect(t, upload(t, f.admin.browser, append(content, '!'), signedBuild(t, "v0.2.0", content)), http.StatusBadRequest, "invalid_input")
		expect(t, upload(t, f.admin.browser, content, nil), http.StatusBadRequest, "invalid_input")
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agent-builds", map[string]string{"build": "x"}), http.StatusBadRequest, "invalid_input")
	})

	t.Run("says why one machine can't update, and turns automatic updates on", func(t *testing.T) {
		f := newTerminalFixture(t)
		got := f.admin.browser.do(http.MethodPost, "/api/agent-updates", api.UpdateAgentsRequest{Target: api.ScriptTarget{Kind: "agent", Value: f.agentID}})
		expect(t, got, http.StatusBadRequest, "invalid_input")
		if !strings.Contains(string(got.body), "no build for linux/amd64") {
			t.Errorf("body = %s", got.body)
		}
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agent-updates", api.UpdateAgentsRequest{Target: api.ScriptTarget{Kind: "agent", Value: "01JBX3K5Q7Z8M4N6P8R0T2V4WM"}}), http.StatusNotFound, "not_found")
		expect(t, f.admin.browser.do(http.MethodPut, "/api/agent-builds/auto-update", api.SetAutoUpdateRequest{Enabled: true}), http.StatusNoContent, "")
		var builds api.AgentBuilds
		f.admin.browser.do(http.MethodGet, "/api/agent-builds", nil).decode(t, &builds)
		if !builds.AutoUpdate {
			t.Error("auto-update stayed off")
		}
	})

}

// SPDX-License-Identifier: Apache-2.0

package updates_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/server/updates"
	"github.com/aazam476/sentinel/internal/shared/release"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func releaseKey(t *testing.T) ed25519.PrivateKey {
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
	return key
}

// fakeHub records update requests; refuse makes the agent refuse.
type fakeHub struct {
	online  map[int64]bool
	asked   []protocol.AgentUpdatePayload
	refusal error
}

func (f *fakeHub) IsOnline(agentID int64) bool { return f.online[agentID] }

func (f *fakeHub) Request(_ context.Context, _ int64, action string, payload, _ any) error {
	if action != protocol.ActionAgentUpdate {
		return errors.New("unexpected action " + action)
	}
	if f.refusal != nil {
		return f.refusal
	}
	f.asked = append(f.asked, payload.(protocol.AgentUpdatePayload))
	return nil
}

type fakeFleet struct{ agents []fleet.Agent }

func (f fakeFleet) AllAgents(context.Context) ([]fleet.Agent, error) { return f.agents, nil }

type harness struct {
	service  *updates.Service
	database *store.Store
	hub      *fakeHub
	clock    *fakeclock.Clock
	key      ed25519.PrivateKey
	dataDir  string
	admin    auth.Session
	agents   []fleet.Agent
	release  *httptest.Server
	files    map[string][]byte
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	clock := fakeclock.New(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	h := &harness{database: database, clock: clock, key: releaseKey(t), dataDir: t.TempDir(), files: map[string][]byte{}}
	err = database.Write(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO users (username, password_hash, role, created_at) VALUES ('ada', 'x', 'admin', 0)`); err != nil {
			return err
		}
		for i, host := range []string{"nas", "pc", "pi"} {
			id := "01JBX3K5Q7Z8M4N6P8R0T2V4W" + string(rune('0'+i))
			result, err := tx.ExecContext(t.Context(), `INSERT INTO agents (public_id, public_key, hostname, os, arch, agent_version, protocol_version, enrolled_at)
				VALUES (?, x'00', ?, 'linux', 'amd64', 'v1.0.0', 1, 0)`, id, host)
			if err != nil {
				return err
			}
			agentID, _ := result.LastInsertId()
			agent := fleet.Agent{ID: agentID, PublicID: id, Hostname: host, OS: "linux", Arch: "amd64", AgentVersion: "v1.0.0", Tags: []string{"Lab"}}
			switch host {
			case "pc":
				agent.OS, agent.Tags = "windows", nil
			case "pi":
				agent.Arch = "arm64"
			}
			h.agents = append(h.agents, agent)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	verified := int64(1)
	h.admin = auth.Session{UserID: 1, Username: "ada", Role: auth.RoleAdmin, SecondFactorAt: &verified}
	h.hub = &fakeHub{online: map[int64]bool{h.agents[0].ID: true, h.agents[1].ID: true}}
	h.release = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, found := h.files[strings.TrimPrefix(r.URL.Path, "/latest/")]
		if !found {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(content)
	}))
	t.Cleanup(h.release.Close)
	h.service = h.newService(h.release.URL + "/latest/")
	return h
}

func (h *harness) newService(releaseURL string) *updates.Service {
	return updates.New(updates.Options{
		DataDir: h.dataDir, Database: h.database, AuditLog: audit.New(h.database), Hub: h.hub, Fleet: fakeFleet{h.agents},
		Clock: h.clock, Keys: []ed25519.PublicKey{h.key.Public().(ed25519.PublicKey)}, ReleaseURL: releaseURL,
	})
}

// build returns a build and its signature.
func (h *harness) build(t *testing.T, version, osName, arch string, content []byte) ([]byte, []byte) {
	t.Helper()
	manifest, err := release.ManifestFor(bytes.NewReader(content), "sentinel-agent", version, osName, arch)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := release.Sign(manifest, h.key)
	if err != nil {
		t.Fatal(err)
	}
	return content, signature
}

func (h *harness) add(t *testing.T, version, osName string) updates.Build {
	t.Helper()
	content, signature := h.build(t, version, osName, "amd64", []byte("agent "+version+" "+osName))
	added, err := h.service.Add(t.Context(), h.admin, auth.RequestInfo{}, bytes.NewReader(content), signature)
	if err != nil {
		t.Fatal(err)
	}
	return added
}

func (h *harness) actions(t *testing.T) []string {
	t.Helper()
	records, err := audit.New(h.database).List(t.Context(), audit.ListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for i := len(records) - 1; i >= 0; i-- {
		actions = append(actions, records[i].Action)
	}
	return actions
}

func TestSEC31_HostedBuilds(t *testing.T) {
	t.Run("hosts signed builds, lists them newest first, and serves them", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "v1.1.0", "linux")
		h.add(t, "v1.10.0", "linux")
		h.add(t, "v1.2.0", "windows")
		builds, err := h.service.Builds()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, b := range builds {
			names = append(names, b.File)
		}
		want := []string{"sentinel-agent_v1.10.0_linux_amd64", "sentinel-agent_v1.2.0_windows_amd64.exe", "sentinel-agent_v1.1.0_linux_amd64"}
		if !slices.Equal(names, want) {
			t.Errorf("got %v", names)
		}
		server := httptest.NewServer(routes(h.service))
		defer server.Close()
		if status := get(t, server.URL+builds[0].Path()); status != http.StatusOK {
			t.Errorf("status %d", status)
		}
		for _, path := range []string{"/install/updates/../secret.key", "/install/updates/v1.10.0/.hidden", "/install/updates/v9/missing"} {
			if status := get(t, server.URL+path); status == http.StatusOK {
				t.Errorf("%s was served", path)
			}
		}
		if got := h.actions(t); !slices.Equal(got, []string{"agent.build.add", "agent.build.add", "agent.build.add"}) {
			t.Errorf("audited %v", got)
		}
	})

	refusals := []struct {
		name   string
		change func(content, signature []byte) ([]byte, []byte)
	}{
		{"an unsigned build", func(c, _ []byte) ([]byte, []byte) { return c, nil }},
		{"a build changed after signing", func(c, s []byte) ([]byte, []byte) { return append(c, '!'), s }},
		{"a build signed by another key", func(c, _ []byte) ([]byte, []byte) {
			_, other, _ := ed25519.GenerateKey(nil)
			manifest, _ := release.ManifestFor(bytes.NewReader(c), "sentinel-agent", "v1.1.0", "linux", "amd64")
			s, _ := release.Sign(manifest, other)
			return c, s
		}},
	}
	for _, tc := range refusals {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			h := newHarness(t)
			content, signature := tc.change(h.build(t, "v1.1.0", "linux", "amd64", []byte("agent")))
			_, err := h.service.Add(t.Context(), h.admin, auth.RequestInfo{}, bytes.NewReader(content), signature)
			var invalid *updates.InvalidError
			if !errors.As(err, &invalid) || invalid.Field != "signature" {
				t.Errorf("got %v", err)
			}
			if builds, _ := h.service.Builds(); len(builds) != 0 {
				t.Errorf("hosted %v", builds)
			}
		})
	}

	t.Run("refuses a server build, and anyone who isn't an admin", func(t *testing.T) {
		h := newHarness(t)
		manifest, _ := release.ManifestFor(bytes.NewReader([]byte("server")), "sentinel-server", "v1.1.0", "linux", "amd64")
		signature, _ := release.Sign(manifest, h.key)
		if _, err := h.service.Add(t.Context(), h.admin, auth.RequestInfo{}, bytes.NewReader([]byte("server")), signature); err == nil {
			t.Error("a server build was hosted as an agent")
		}
		operator := h.admin
		operator.Role = auth.RoleOperator
		if _, err := h.service.Add(t.Context(), operator, auth.RequestInfo{}, bytes.NewReader(nil), nil); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("stops listing a build whose signature no longer checks out", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "v1.1.0", "linux")
		_, other, _ := ed25519.GenerateKey(nil)
		rotated := updates.New(updates.Options{DataDir: h.dataDir, Keys: []ed25519.PublicKey{other.Public().(ed25519.PublicKey)}, Clock: h.clock})
		if builds, _ := rotated.Builds(); len(builds) != 0 {
			t.Errorf("listed %v", builds)
		}
	})
}

func routes(service *updates.Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /install/updates/{version}/{file}", service.Handler())
	return mux
}

func TestUpdate(t *testing.T) {
	t.Run("asks each online machine with a build to update to the newest, and says why it skipped the rest", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "v1.1.0", "linux")
		newest := h.add(t, "v1.2.0", "linux")
		outcome, err := h.service.Update(t.Context(), h.admin, auth.RequestInfo{}, updates.Target{Kind: "all"}, "")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(outcome.Started, []string{h.agents[0].PublicID}) {
			t.Errorf("started %v", outcome.Started)
		}
		if outcome.Skipped[h.agents[1].PublicID] != "there is no build for windows/amd64" || outcome.Skipped[h.agents[2].PublicID] != "it is offline" {
			t.Errorf("skipped %v", outcome.Skipped)
		}
		if len(h.hub.asked) != 1 || h.hub.asked[0].Version != "v1.2.0" || h.hub.asked[0].Path != newest.Path() ||
			!strings.Contains(h.hub.asked[0].Signature, "sentinel-signature v1") {
			t.Errorf("asked %+v", h.hub.asked)
		}
		statuses, _ := h.service.Statuses(t.Context())
		if status := statuses[h.agents[0].ID]; status.State != updates.StateUpdating || status.Version != "v1.2.0" {
			t.Errorf("status %+v", status)
		}
		if got := h.actions(t); got[len(got)-1] != "agent.update.start" {
			t.Errorf("audited %v", got)
		}
	})

	t.Run("updates a tag, or one machine to a chosen version, and says why one machine can't", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "v1.1.0", "linux")
		h.add(t, "v1.2.0", "linux")
		outcome, err := h.service.Update(t.Context(), h.admin, auth.RequestInfo{}, updates.Target{Kind: "tag", Value: "lab"}, "v1.1.0")
		if err != nil || len(outcome.Started) != 1 || len(outcome.Skipped) != 1 || h.hub.asked[0].Version != "v1.1.0" {
			t.Errorf("got %+v, %v", outcome, err)
		}
		h.agents[0].AgentVersion = "v1.2.0"
		h.service = h.newService("")
		var invalid *updates.InvalidError
		if _, err := h.service.Update(t.Context(), h.admin, auth.RequestInfo{}, updates.Target{Kind: "agent", Value: h.agents[0].PublicID}, ""); !errors.As(err, &invalid) || !strings.Contains(invalid.Reason, "already runs v1.2.0") {
			t.Errorf("got %v", err)
		}
		if _, err := h.service.Update(t.Context(), h.admin, auth.RequestInfo{}, updates.Target{Kind: "agent", Value: "01JBX3K5Q7Z8M4N6P8R0T2V4WZ"}, ""); !errors.Is(err, updates.ErrNotFound) {
			t.Errorf("got %v", err)
		}
		if _, err := h.service.Update(t.Context(), h.admin, auth.RequestInfo{}, updates.Target{Kind: "everyone"}, ""); !errors.As(err, &invalid) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("passes on a machine's refusal", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "v1.1.0", "linux")
		h.hub.refusal = &protocol.Error{Code: protocol.ErrorConflict, Message: "An update is already in progress."}
		outcome, _ := h.service.Update(t.Context(), h.admin, auth.RequestInfo{}, updates.Target{Kind: "all"}, "")
		if outcome.Skipped[h.agents[0].PublicID] != "An update is already in progress." {
			t.Errorf("skipped %v", outcome.Skipped)
		}
		h.hub.refusal = errors.New("connection closed")
		outcome, _ = h.service.Update(t.Context(), h.admin, auth.RequestInfo{}, updates.Target{Kind: "all"}, "")
		if !strings.Contains(outcome.Skipped[h.agents[0].PublicID], "connection closed") {
			t.Errorf("skipped %v", outcome.Skipped)
		}
	})

	t.Run("refuses anyone who isn't an admin", func(t *testing.T) {
		h := newHarness(t)
		viewer := h.admin
		viewer.Role = auth.RoleViewer
		if _, err := h.service.Update(t.Context(), viewer, auth.RequestInfo{}, updates.Target{Kind: "all"}, ""); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("got %v", err)
		}
		if err := h.service.SetAutoUpdate(t.Context(), viewer, auth.RequestInfo{}, true); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("got %v", err)
		}
		if _, err := h.service.Fetch(t.Context(), viewer, auth.RequestInfo{}); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("got %v", err)
		}
	})
}

func TestSEC32_RecordResult(t *testing.T) {
	h := newHarness(t)
	agent := h.agents[0].ID
	cases := []struct {
		result protocol.AgentUpdateResultPayload
		state  string
	}{
		{protocol.AgentUpdateResultPayload{Version: "v1.1.0", OK: true}, updates.StateUpdated},
		{protocol.AgentUpdateResultPayload{Version: "v1.2.0", Error: "the new version failed before connecting"}, updates.StateFailed},
		{protocol.AgentUpdateResultPayload{Version: "v1.3.0", RolledBack: true, Error: "didn't connect"}, updates.StateRolledBack},
	}
	for _, tc := range cases {
		if err := h.service.RecordResult(t.Context(), agent, tc.result); err != nil {
			t.Fatal(err)
		}
		statuses, err := h.service.Statuses(t.Context())
		status := statuses[agent]
		if err != nil || status.State != tc.state || status.Version != tc.result.Version || status.Error != tc.result.Error || status.FinishedAt == nil {
			t.Errorf("%s: got %+v, %v", tc.state, status, err)
		}
	}
	records, _ := audit.New(h.database).List(t.Context(), audit.ListOptions{Limit: 10})
	if len(records) != 3 || records[0].Action != "agent.update.result" || records[0].Outcome != audit.OutcomeFailure || records[2].Outcome != audit.OutcomeSuccess {
		t.Errorf("audited %+v", records)
	}
}

func TestAutoUpdate(t *testing.T) {
	h := newHarness(t)
	h.add(t, "v1.1.0", "linux")
	if err := h.service.RunAuto(t.Context()); err != nil || len(h.hub.asked) != 0 {
		t.Fatalf("updated while auto-update was off: %v, %v", h.hub.asked, err)
	}
	if err := h.service.SetAutoUpdate(t.Context(), h.admin, auth.RequestInfo{}, true); err != nil {
		t.Fatal(err)
	}
	if enabled, err := h.service.AutoUpdate(t.Context()); err != nil || !enabled {
		t.Fatalf("got %v, %v", enabled, err)
	}
	if err := h.service.RunAuto(t.Context()); err != nil || len(h.hub.asked) != 1 {
		t.Fatalf("asked %v, %v", h.hub.asked, err)
	}
	// A machine whose update to this version is under way, or failed, isn't asked again.
	if err := h.service.RunAuto(t.Context()); err != nil || len(h.hub.asked) != 1 {
		t.Errorf("asked again: %v, %v", h.hub.asked, err)
	}
	if err := h.service.RecordResult(t.Context(), h.agents[0].ID, protocol.AgentUpdateResultPayload{Version: "v1.1.0", Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	if err := h.service.RunAuto(t.Context()); err != nil || len(h.hub.asked) != 1 {
		t.Errorf("asked again after a failure: %v, %v", h.hub.asked, err)
	}
	if got := h.actions(t); !slices.Contains(got, "agent.update.auto") {
		t.Errorf("audited %v", got)
	}
}

func TestFetch(t *testing.T) {
	publish := func(h *harness) {
		var sums strings.Builder
		for _, platform := range [][2]string{{"linux", "amd64"}, {"windows", "amd64"}} {
			content, signature := h.build(t, "v2.0.0", platform[0], platform[1], []byte("agent v2 "+platform[0]))
			name := "sentinel-agent_v2.0.0_" + platform[0] + "_" + platform[1]
			if platform[0] == "windows" {
				name += ".exe"
			}
			h.files[name], h.files[name+".sig"] = content, signature
			manifest, _ := release.ManifestFor(bytes.NewReader(content), "", "v", "any", "any")
			sums.WriteString(hexOf(manifest.SHA256[:]) + "  " + name + "\n")
			sums.WriteString(hexOf(manifest.SHA256[:]) + "  " + name + ".spdx.json\n")
		}
		sums.WriteString("0000  sentinel-server_v2.0.0_linux_amd64\n")
		h.files["SHA256SUMS"] = []byte(sums.String())
		manifest, _ := release.ManifestFor(strings.NewReader(sums.String()), "SHA256SUMS", "v2.0.0", "any", "any")
		h.files["SHA256SUMS.sig"], _ = release.Sign(manifest, h.key)
	}

	t.Run("hosts the agent builds of the latest release, checked against its signed checksums", func(t *testing.T) {
		h := newHarness(t)
		publish(h)
		added, err := h.service.Fetch(t.Context(), h.admin, auth.RequestInfo{})
		if err != nil || len(added) != 2 {
			t.Fatalf("got %v, %v", added, err)
		}
		if got := h.actions(t); !slices.Equal(got, []string{"agent.build.fetch"}) {
			t.Errorf("audited %v", got)
		}
	})

	failures := []struct {
		name   string
		change func(h *harness)
		want   string
	}{
		{"a checksum list signed by someone else", func(h *harness) { h.files["SHA256SUMS"] = append(h.files["SHA256SUMS"], '\n') }, "checksum list was refused"},
		{"a build that doesn't match its checksum", func(h *harness) { h.files["sentinel-agent_v2.0.0_linux_amd64"] = []byte("swapped") }, "doesn't match"},
		{"a missing signature", func(h *harness) { delete(h.files, "sentinel-agent_v2.0.0_linux_amd64.sig") }, "404"},
		{"a missing checksum list", func(h *harness) { delete(h.files, "SHA256SUMS") }, "404"},
		{"a missing checksum signature", func(h *harness) { delete(h.files, "SHA256SUMS.sig") }, "404"},
	}
	for _, tc := range failures {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			h := newHarness(t)
			publish(h)
			tc.change(h)
			if _, err := h.service.Fetch(t.Context(), h.admin, auth.RequestInfo{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("refuses with no release channel, or one it can't reach", func(t *testing.T) {
		h := newHarness(t)
		if h.newService("").CanFetch() {
			t.Error("CanFetch with no release channel")
		}
		if _, err := h.newService("").Fetch(t.Context(), h.admin, auth.RequestInfo{}); err == nil {
			t.Error("fetched with no release channel")
		}
		if _, err := h.newService("http://[::1").Fetch(t.Context(), h.admin, auth.RequestInfo{}); err == nil {
			t.Error("fetched from a broken address")
		}
		h.release.Close()
		if _, err := h.service.Fetch(t.Context(), h.admin, auth.RequestInfo{}); err == nil {
			t.Error("fetched from a server that's gone")
		}
	})
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

// get fetches url and returns the status.
func get(t *testing.T, url string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

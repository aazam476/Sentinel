// SPDX-License-Identifier: Apache-2.0

package update_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/update"
	"github.com/aazam476/sentinel/internal/protocol"
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

// trialOutcome is what the fake trial does.
type trialOutcome int

const (
	trialConnects trialOutcome = iota
	trialFails
	trialHangs
	trialExitsQuietly
)

type harness struct {
	updater    *update.Updater
	executable string
	stateDir   string
	clock      *fakeclock.Clock
	server     *httptest.Server
	key        ed25519.PrivateKey
	builds     map[string][]byte
	trialArgs  []string
	outcome    trialOutcome
	paused     int
	resumed    int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{
		executable: filepath.Join(dir, "sentinel-agent"), stateDir: t.TempDir(),
		clock: fakeclock.New(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)), key: releaseKey(t), builds: map[string][]byte{},
	}
	if err := os.WriteFile(h.executable, []byte("old build"), 0o755); err != nil { //nolint:gosec // G306: an executable
		t.Fatal(err)
	}
	h.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		build, found := h.builds[r.URL.Path]
		if !found {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(build)
	}))
	t.Cleanup(h.server.Close)
	h.updater = update.New(update.Options{
		Executable: h.executable, StateDir: h.stateDir, ServerURL: h.server.URL, HTTPClient: h.server.Client(),
		Keys: []ed25519.PublicKey{h.key.Public().(ed25519.PublicKey)}, Version: "v1.0.0", OS: "linux", Arch: "amd64",
		RunTrial: h.runTrial, TrialArgs: []string{"--state-dir", "s"}, Clock: h.clock, Logger: slog.New(slog.DiscardHandler),
	})
	return h
}

func (h *harness) runTrial(ctx context.Context, executable string, args []string) error {
	h.trialArgs = append([]string{executable}, args...)
	switch h.outcome {
	case trialConnects:
		return os.WriteFile(args[len(args)-1], []byte("connected v1.1.0\n"), 0o600)
	case trialFails:
		return errors.New("exit status 2")
	case trialExitsQuietly:
		return nil
	case trialHangs:
	}
	<-ctx.Done()
	return ctx.Err()
}

// publish serves a build signed as the agent for the platform given.
func (h *harness) publish(t *testing.T, version, osName string, content []byte) protocol.AgentUpdatePayload {
	t.Helper()
	manifest, err := release.ManifestFor(bytes.NewReader(content), "sentinel-agent", version, osName, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := release.Sign(manifest, h.key)
	if err != nil {
		t.Fatal(err)
	}
	path := "/install/updates/" + version + "/sentinel-agent_" + version + "_" + osName + "_amd64"
	h.builds[path] = content
	return protocol.AgentUpdatePayload{Version: version, Path: path, Signature: string(signature)}
}

func (h *harness) pause() func() {
	h.paused++
	return func() { h.resumed++ }
}

func (h *harness) installed(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(h.executable)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestSEC31_Update(t *testing.T) {
	t.Run("installs a signed build once its trial connects, keeping the previous one", func(t *testing.T) {
		h := newHarness(t)
		request := h.publish(t, "v1.1.0", "linux", []byte("new build"))
		if err := h.updater.Update(t.Context(), request, h.pause); err != nil {
			t.Fatal(err)
		}
		if got := h.installed(t); got != "new build" {
			t.Errorf("installed %q", got)
		}
		if previous, _ := os.ReadFile(h.executable + ".previous"); string(previous) != "old build" {
			t.Errorf("previous = %q", previous)
		}
		want := []string{h.executable + ".new", "run", "--state-dir", "s", "--update-trial", filepath.Join(h.stateDir, "update-trial")}
		if strings.Join(h.trialArgs, " ") != strings.Join(want, " ") {
			t.Errorf("trial ran %v", h.trialArgs)
		}
		if h.paused != 1 || h.resumed != 0 {
			t.Errorf("paused %d, resumed %d", h.paused, h.resumed)
		}
	})

	refusals := []struct {
		name    string
		request func(h *harness) protocol.AgentUpdatePayload
		want    string
	}{
		{"an unsigned build", func(h *harness) protocol.AgentUpdatePayload {
			r := h.publish(t, "v1.1.0", "linux", []byte("new build"))
			r.Signature = ""
			return r
		}, "refused"},
		{"a build changed after signing", func(h *harness) protocol.AgentUpdatePayload {
			r := h.publish(t, "v1.1.0", "linux", []byte("new build"))
			h.builds[r.Path] = []byte("tampered build")
			return r
		}, "refused"},
		{"a build for another system", func(h *harness) protocol.AgentUpdatePayload {
			return h.publish(t, "v1.1.0", "windows", []byte("new build"))
		}, "refused"},
		{"a build of another version than asked", func(h *harness) protocol.AgentUpdatePayload {
			r := h.publish(t, "v1.1.0", "linux", []byte("new build"))
			r.Version = "v1.2.0"
			return r
		}, "refused"},
		{"the version already running", func(h *harness) protocol.AgentUpdatePayload {
			return h.publish(t, "v1.0.0", "linux", []byte("same"))
		}, "already runs"},
		{"a path off the server's install folder", func(h *harness) protocol.AgentUpdatePayload {
			r := h.publish(t, "v1.1.0", "linux", []byte("new build"))
			r.Path = "/install/../etc/passwd"
			return r
		}, "malformed"},
		{"a build the server doesn't have", func(h *harness) protocol.AgentUpdatePayload {
			r := h.publish(t, "v1.1.0", "linux", []byte("new build"))
			r.Path = "/install/missing"
			return r
		}, "404"},
	}
	for _, tc := range refusals {
		t.Run("refuses "+tc.name+" and keeps the running version", func(t *testing.T) {
			h := newHarness(t)
			err := h.updater.Update(t.Context(), tc.request(h), h.pause)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
			if got := h.installed(t); got != "old build" || h.paused != 0 {
				t.Errorf("installed %q, paused %d", got, h.paused)
			}
			if _, err := os.Stat(h.executable + ".new"); err == nil {
				t.Error("the refused build was kept")
			}
		})
	}
}

func TestSEC32_Rollback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome trialOutcome
		want    string
	}{
		{"a new version that crashes before connecting", trialFails, "failed before connecting"},
	} {
		t.Run("discards "+tc.name+" and resumes", func(t *testing.T) {
			h := newHarness(t)
			h.outcome = tc.outcome
			err := h.updater.Update(t.Context(), h.publish(t, "v1.1.0", "linux", []byte("broken build")), h.pause)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
			if got := h.installed(t); got != "old build" || h.resumed != 1 {
				t.Errorf("installed %q, resumed %d", got, h.resumed)
			}
			h.updater.Failed("v1.1.0", err)
			report := h.updater.Connected()
			if report == nil || report.OK || report.Version != "v1.1.0" || !strings.Contains(report.Error, tc.want) {
				t.Errorf("report = %+v", report)
			}
			if again := h.updater.Connected(); again != nil {
				t.Errorf("reported twice: %+v", again)
			}
		})
	}

	t.Run("gives up on a trial that never connects", func(t *testing.T) {
		h := newHarness(t)
		h.outcome = trialHangs
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		err := h.updater.Update(ctx, h.publish(t, "v1.1.0", "linux", []byte("silent build")), h.pause)
		if err == nil || !strings.Contains(err.Error(), "didn't connect") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("after install, the new version confirms itself once connected, then reports success", func(t *testing.T) {
		h := newHarness(t)
		if err := h.updater.Update(t.Context(), h.publish(t, "v1.1.0", "linux", []byte("new build")), h.pause); err != nil {
			t.Fatal(err)
		}
		next := h.restartAs("v1.1.0")
		if !next.Watch() {
			t.Fatal("the new version isn't watching itself")
		}
		if report := next.Connected(); report == nil || !report.OK || report.Version != "v1.1.0" {
			t.Errorf("report = %+v", report)
		}
		if next.Watch() {
			t.Error("still watching after connecting")
		}
		h.clock.Advance(update.KeepPrevious + time.Minute)
		next.Watch()
		if _, err := os.Stat(h.executable + ".previous"); err == nil {
			t.Error("the previous version was kept for more than a day")
		}
	})

	t.Run("after install, a new version that can't connect puts the previous one back and says so", func(t *testing.T) {
		h := newHarness(t)
		if err := h.updater.Update(t.Context(), h.publish(t, "v1.1.0", "linux", []byte("new build")), h.pause); err != nil {
			t.Fatal(err)
		}
		next := h.restartAs("v1.1.0")
		if !next.Watch() {
			t.Fatal("the new version isn't watching itself")
		}
		if err := next.Rollback(); err != nil {
			t.Fatal(err)
		}
		if got := h.installed(t); got != "old build" {
			t.Errorf("installed %q", got)
		}
		old := h.restartAs("v1.0.0")
		if old.Watch() {
			t.Error("the old version is watching itself")
		}
		report := old.Connected()
		if report == nil || report.OK || !report.RolledBack || report.Version != "v1.1.0" {
			t.Errorf("report = %+v", report)
		}
		if _, err := os.Stat(h.executable + ".failed"); err == nil {
			t.Error("the failed build is still there")
		}
	})
}

// restartAs is the updater the agent has after restarting as version.
func (h *harness) restartAs(version string) *update.Updater {
	return update.New(update.Options{
		Executable: h.executable, StateDir: h.stateDir, Version: version, Clock: h.clock, Logger: slog.New(slog.DiscardHandler),
		Keys: []ed25519.PublicKey{h.key.Public().(ed25519.PublicKey)},
	})
}

func TestFailures(t *testing.T) {
	t.Run("reports a trial that exits before connecting", func(t *testing.T) {
		h := newHarness(t)
		h.outcome = trialExitsQuietly
		if err := h.updater.Update(t.Context(), h.publish(t, "v1.1.0", "linux", []byte("new")), h.pause); err == nil || !strings.Contains(err.Error(), "stopped before connecting") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("refuses a download bigger than any build", func(t *testing.T) {
		h := newHarness(t)
		small := update.New(update.Options{
			Executable: h.executable, StateDir: h.stateDir, ServerURL: h.server.URL, HTTPClient: h.server.Client(),
			Version: "v1.0.0", OS: "linux", Arch: "amd64", MaxSize: 3, Clock: h.clock, Logger: slog.New(slog.DiscardHandler),
		})
		if err := small.Update(t.Context(), h.publish(t, "v1.1.0", "linux", []byte("too big")), h.pause); err == nil || !strings.Contains(err.Error(), "larger") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("reports a server it can't reach, or whose address is broken", func(t *testing.T) {
		h := newHarness(t)
		request := h.publish(t, "v1.1.0", "linux", []byte("new"))
		h.server.Close()
		if err := h.updater.Update(t.Context(), request, h.pause); err == nil || !strings.Contains(err.Error(), "download") {
			t.Errorf("got %v", err)
		}
		broken := update.New(update.Options{Executable: h.executable, ServerURL: "http://[::1", Version: "v1.0.0", Clock: h.clock})
		if err := broken.Update(t.Context(), request, h.pause); err == nil || !strings.Contains(err.Error(), "server address") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("reports a download cut short", func(t *testing.T) {
		h := newHarness(t)
		request := h.publish(t, "v1.1.0", "linux", []byte("new"))
		cut := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("partial"))
		}))
		defer cut.Close()
		updater := update.New(update.Options{Executable: h.executable, ServerURL: cut.URL, HTTPClient: cut.Client(), Version: "v1.0.0", Clock: h.clock})
		if err := updater.Update(t.Context(), request, h.pause); err == nil || !strings.Contains(err.Error(), "download") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("keeps the running version when the new one can't be saved or installed", func(t *testing.T) {
		for _, blocked := range []string{".new", ".previous"} {
			h := newHarness(t)
			// A folder with a file in it can't be removed or replaced.
			if err := os.MkdirAll(filepath.Join(h.executable+blocked, "x"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := h.updater.Update(t.Context(), h.publish(t, "v1.1.0", "linux", []byte("new")), h.pause); err == nil {
				t.Errorf("%s: expected an error", blocked)
			}
			if got := h.installed(t); got != "old build" {
				t.Errorf("%s: installed %q", blocked, got)
			}
		}
	})

	t.Run("reports a rollback with nothing to roll back to", func(t *testing.T) {
		h := newHarness(t)
		if err := h.restartAs("v1.1.0").Rollback(); err == nil {
			t.Error("expected an error")
		}
		if got := h.installed(t); got != "old build" {
			t.Errorf("installed %q", got)
		}
		gone := update.New(update.Options{Executable: filepath.Join(t.TempDir(), "missing"), StateDir: h.stateDir, Clock: h.clock})
		if err := gone.Rollback(); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("ignores an update record it can't read", func(t *testing.T) {
		h := newHarness(t)
		if err := os.WriteFile(filepath.Join(h.stateDir, "update.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		if h.updater.Watch() || h.updater.Connected() != nil {
			t.Error("acted on a broken record")
		}
	})

	t.Run("logs, rather than fails, when it can't record a failed update", func(t *testing.T) {
		updater := update.New(update.Options{StateDir: filepath.Join(t.TempDir(), "missing"), Clock: fakeclock.New(time.Now()), Logger: slog.New(slog.DiscardHandler)})
		updater.Failed("v1.1.0", errors.New("refused"))
	})

	t.Run("trusts the compiled-in keys by default", func(t *testing.T) {
		h := newHarness(t)
		updater := update.New(update.Options{
			Executable: h.executable, StateDir: h.stateDir, ServerURL: h.server.URL, HTTPClient: h.server.Client(),
			Version: "v1.0.0", OS: "linux", Arch: "amd64", RunTrial: h.runTrial, Clock: h.clock,
		})
		err := updater.Update(t.Context(), h.publish(t, "v1.1.0", "linux", []byte("new")), h.pause)
		if release.IsDevelopmentBuild() && err != nil {
			t.Errorf("a build signed with the development key was refused: %v", err)
		}
	})
}

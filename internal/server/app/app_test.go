// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aazam476/sentinel/internal/server/app"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

func TestNew(t *testing.T) {
	t.Run("creates the data directory, database and key, and serves requests", func(t *testing.T) {
		dataDir := filepath.Join(t.TempDir(), "data")
		server := newApp(t, dataDir)

		for _, name := range []string{"sentinel.db", "secret.key"} {
			if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
		recorder := httptest.NewRecorder()
		server.Handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
		if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("GET /healthz = %d with headers %v, want 200 with security headers", recorder.Code, recorder.Header())
		}
	})

	t.Run("serves the embedded web UI by default", func(t *testing.T) {
		server := newApp(t, t.TempDir())
		recorder := httptest.NewRecorder()
		server.Handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		// 503 when the UI wasn't built before the tests; either way it is
		// the web UI answering, not the API.
		if recorder.Code != http.StatusOK && recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("GET / = %d, want the web UI", recorder.Code)
		}
	})

	t.Run("fails when the data directory can't be created", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := app.New(t.Context(), options(t, filepath.Join(file, "data"))); err == nil {
			t.Error("New returned nil")
		}
	})

	t.Run("fails when the secrets key can't be used", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dataDir, "secret.key"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := app.New(t.Context(), options(t, dataDir)); err == nil {
			t.Error("New returned nil")
		}
	})

	t.Run("fails when the database can't be opened", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dataDir, "sentinel.db"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := app.New(t.Context(), options(t, dataDir)); err == nil {
			t.Error("New returned nil")
		}
	})

	t.Run("sends HSTS only with an ACME certificate", func(t *testing.T) {
		settings := options(t, t.TempDir())
		settings.Settings.TLS.Mode = config.TLSACME
		server, err := app.New(t.Context(), settings)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = server.Close() }) // nothing to check when a test database closes
		recorder := httptest.NewRecorder()
		server.Handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
		if recorder.Header().Get("Strict-Transport-Security") == "" {
			t.Error("no HSTS header in ACME mode")
		}
	})

}

func options(t *testing.T, dataDir string) app.Options {
	t.Helper()
	settings, err := config.File{DataDir: dataDir}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return app.Options{Settings: settings, Logger: slog.New(slog.DiscardHandler), Clock: clock.System{}}
}

func newApp(t *testing.T, dataDir string) *app.App {
	t.Helper()
	server, err := app.New(t.Context(), options(t, dataDir))
	if err != nil {
		t.Fatalf("New returned %v", err)
	}
	t.Cleanup(func() { _ = server.Close() }) // nothing to check when a test database closes
	return server
}

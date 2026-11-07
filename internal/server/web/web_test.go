// SPDX-License-Identifier: Apache-2.0

package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/aazam476/sentinel/internal/server/web"
)

// builtUI returns an in-memory copy of what a Vite build produces.
func builtUI() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>Sentinel</title><div id=\"root\"></div>")},
		"assets/index.js":    {Data: []byte("console.log('hello')")},
		"assets/index.css":   {Data: []byte("body{}")},
		"assets/nested/a.js": {Data: []byte("1")},
	}
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name            string
		files           fstest.MapFS
		path            string
		wantStatus      int
		wantBodyContain string
		wantContentType string
	}{
		{
			name:            "serves index.html at the root",
			files:           builtUI(),
			path:            "/",
			wantStatus:      http.StatusOK,
			wantBodyContain: `<div id="root"></div>`,
			wantContentType: "text/html; charset=utf-8",
		},
		{
			name:            "serves a built asset",
			files:           builtUI(),
			path:            "/assets/index.js",
			wantStatus:      http.StatusOK,
			wantBodyContain: "console.log",
			wantContentType: "text/javascript; charset=utf-8",
		},
		{
			name:            "serves a stylesheet as text/css",
			files:           builtUI(),
			path:            "/assets/index.css",
			wantStatus:      http.StatusOK,
			wantBodyContain: "body{}",
			wantContentType: "text/css; charset=utf-8",
		},
		{
			name:       "returns not found for a missing file",
			files:      builtUI(),
			path:       "/assets/missing.js",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "does not list the contents of a folder",
			files:      builtUI(),
			path:       "/assets/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "does not list the contents of a nested folder",
			files:      builtUI(),
			path:       "/assets/nested/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:            "explains how to build the UI when it is missing",
			files:           fstest.MapFS{".gitkeep": {Data: nil}},
			path:            "/",
			wantStatus:      http.StatusServiceUnavailable,
			wantBodyContain: "make build",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "explains how to build the UI for any path when it is missing",
			files:           fstest.MapFS{},
			path:            "/assets/index.js",
			wantStatus:      http.StatusServiceUnavailable,
			wantBodyContain: "make build",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := web.NewHandler(test.files)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, nil))

			if recorder.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			body, _ := io.ReadAll(recorder.Body) // reading a recorder's buffer cannot fail
			if !strings.Contains(string(body), test.wantBodyContain) {
				t.Errorf("body = %q, want it to contain %q", body, test.wantBodyContain)
			}
			if test.wantContentType != "" && recorder.Header().Get("Content-Type") != test.wantContentType {
				t.Errorf("Content-Type = %q, want %q", recorder.Header().Get("Content-Type"), test.wantContentType)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	t.Run("serves the embedded UI, or explains how to build it", func(t *testing.T) {
		handler, err := web.Handler()
		if err != nil {
			t.Fatalf("Handler returned %v", err)
		}
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

		// Whether the UI is embedded depends on whether `make web` ran before
		// the tests, so either outcome is correct; anything else is a bug.
		if recorder.Code != http.StatusOK && recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 200 or 503", recorder.Code)
		}
	})
}

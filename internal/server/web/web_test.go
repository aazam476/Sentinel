// SPDX-License-Identifier: Apache-2.0

package web_test

import (
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
		"index.html":         {Data: []byte("<!doctype html><html><head><title>Sentinel</title></head><body><div id=\"root\"></div></body></html>")},
		"favicon.svg":        {Data: []byte("<svg></svg>")},
		"assets/index.js":    {Data: []byte("console.log('hello')")},
		"assets/index.css":   {Data: []byte("body{}")},
		"assets/nested/a.js": {Data: []byte("1")},
	}
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name             string
		files            fstest.MapFS
		method           string
		path             string
		wantStatus       int
		wantBodyContain  string
		wantContentType  string
		wantCacheControl string
	}{
		{
			name:             "serves index.html at the root without caching it",
			files:            builtUI(),
			path:             "/",
			wantStatus:       http.StatusOK,
			wantBodyContain:  `<div id="root"></div>`,
			wantContentType:  "text/html; charset=utf-8",
			wantCacheControl: "no-cache",
		},
		{
			name:            "serves index.html when asked for it by name",
			files:           builtUI(),
			path:            "/index.html",
			wantStatus:      http.StatusOK,
			wantBodyContain: `<div id="root"></div>`,
		},
		{
			name:            "serves index.html for a page of the app",
			files:           builtUI(),
			path:            "/login",
			wantStatus:      http.StatusOK,
			wantBodyContain: `<div id="root"></div>`,
		},
		{
			name:            "serves index.html for a nested page of the app",
			files:           builtUI(),
			path:            "/machines/42/terminal",
			wantStatus:      http.StatusOK,
			wantBodyContain: `<div id="root"></div>`,
		},
		{
			name:             "serves a built asset with a long cache lifetime",
			files:            builtUI(),
			path:             "/assets/index.js",
			wantStatus:       http.StatusOK,
			wantBodyContain:  "console.log",
			wantContentType:  "text/javascript; charset=utf-8",
			wantCacheControl: "public, max-age=31536000, immutable",
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
			name:            "serves a file outside assets without long caching",
			files:           builtUI(),
			path:            "/favicon.svg",
			wantStatus:      http.StatusOK,
			wantContentType: "image/svg+xml",
		},
		{
			name:       "returns not found for a missing file with an extension",
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
			name:            "serves the app for a folder name without a slash",
			files:           builtUI(),
			path:            "/assets",
			wantStatus:      http.StatusOK,
			wantBodyContain: `<div id="root"></div>`,
		},
		{
			name:       "refuses methods other than GET and HEAD",
			files:      builtUI(),
			method:     http.MethodPost,
			path:       "/",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:            "explains how to build the UI when it is missing",
			files:           fstest.MapFS{".gitkeep": {Data: nil}},
			path:            "/",
			wantStatus:      http.StatusServiceUnavailable,
			wantBodyContain: "make build",
			wantContentType: "text/plain; charset=utf-8",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			method := test.method
			if method == "" {
				method = http.MethodGet
			}
			recorder := serve(t, web.NewHandler(test.files, "/"), method, test.path)

			if recorder.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if !strings.Contains(recorder.Body.String(), test.wantBodyContain) {
				t.Errorf("body = %q, want it to contain %q", recorder.Body.String(), test.wantBodyContain)
			}
			if test.wantContentType != "" && recorder.Header().Get("Content-Type") != test.wantContentType {
				t.Errorf("Content-Type = %q, want %q", recorder.Header().Get("Content-Type"), test.wantContentType)
			}
			if test.wantCacheControl != "" && recorder.Header().Get("Cache-Control") != test.wantCacheControl {
				t.Errorf("Cache-Control = %q, want %q", recorder.Header().Get("Cache-Control"), test.wantCacheControl)
			}
		})
	}
}

func TestSEC14_PageWorksUnderABasePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		want     string
	}{
		{name: "points relative links at the root", basePath: "/", want: `<base href="/" />`},
		{name: "points relative links at the base path", basePath: "/sentinel", want: `<base href="/sentinel/" />`},
		{name: "handles nested base paths", basePath: "/tools/sentinel", want: `<base href="/tools/sentinel/" />`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := serve(t, web.NewHandler(builtUI(), test.basePath), http.MethodGet, "/")

			if !strings.Contains(recorder.Body.String(), "<head>"+test.want) {
				t.Errorf("body = %q, want it to start the head with %q", recorder.Body.String(), test.want)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	t.Run("serves the embedded UI, or explains how to build it", func(t *testing.T) {
		handler, err := web.Handler("/")
		if err != nil {
			t.Fatalf("Handler returned %v", err)
		}

		recorder := serve(t, handler, http.MethodGet, "/")

		// Whether the UI is embedded depends on whether `make web` ran before
		// the tests, so either outcome is correct; anything else is a bug.
		if recorder.Code != http.StatusOK && recorder.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 200 or 503", recorder.Code)
		}
	})
}

func serve(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), method, path, nil))
	return recorder
}

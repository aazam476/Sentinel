// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/api"
)

func TestRouter(t *testing.T) {
	webUI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "web ui") // a failed write fails the assertion below
	})
	router := api.NewRouter(api.RouterOptions{WebUI: webUI})

	tests := []struct {
		name            string
		method          string
		path            string
		wantStatus      int
		wantBody        string
		wantContentType string
	}{
		{name: "healthz answers ok without logging in", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantBody: "ok", wantContentType: "text/plain; charset=utf-8"},
		{name: "healthz only answers GET", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
		{name: "an unknown API path is a JSON 404", method: http.MethodGet, path: "/api/nothing-here", wantStatus: http.StatusNotFound, wantBody: `"code":"not_found"`, wantContentType: "application/json"},
		{name: "other paths go to the web UI", method: http.MethodGet, path: "/login", wantStatus: http.StatusOK, wantBody: "web ui"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil))

			if recorder.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if !strings.Contains(recorder.Body.String(), test.wantBody) {
				t.Errorf("body = %q, want it to contain %q", recorder.Body.String(), test.wantBody)
			}
			if test.wantContentType != "" && recorder.Header().Get("Content-Type") != test.wantContentType {
				t.Errorf("Content-Type = %q, want %q", recorder.Header().Get("Content-Type"), test.wantContentType)
			}
		})
	}
}

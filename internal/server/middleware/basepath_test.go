// SPDX-License-Identifier: Apache-2.0

package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aazam476/sentinel/internal/server/middleware"
)

func TestSEC14_ServesUnderAConfiguredBasePath(t *testing.T) {

	tests := []struct {
		name         string
		basePath     string
		requestPath  string
		wantStatus   int
		wantPath     string
		wantLocation string
	}{
		{name: "passes everything through at the root", basePath: "/", requestPath: "/api/x", wantStatus: http.StatusOK, wantPath: "/api/x"},
		{name: "strips the base path", basePath: "/sentinel", requestPath: "/sentinel/api/x", wantStatus: http.StatusOK, wantPath: "/api/x"},
		{name: "maps the base path with a slash to the root", basePath: "/sentinel", requestPath: "/sentinel/", wantStatus: http.StatusOK, wantPath: "/"},
		{name: "redirects the bare base path to add a slash", basePath: "/sentinel", requestPath: "/sentinel", wantStatus: http.StatusPermanentRedirect, wantLocation: "/sentinel/"},
		{name: "returns not found outside the base path", basePath: "/sentinel", requestPath: "/api/x", wantStatus: http.StatusNotFound},
		{name: "does not match a longer path with the same start", basePath: "/sentinel", requestPath: "/sentinel-other/x", wantStatus: http.StatusNotFound},
		{name: "handles nested base paths", basePath: "/tools/sentinel", requestPath: "/tools/sentinel/healthz", wantStatus: http.StatusOK, wantPath: "/healthz"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var seenPath string
			handler := middleware.WithBasePath(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenPath = r.URL.Path
			}), test.basePath)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.requestPath, nil))

			if recorder.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if test.wantPath != "" && seenPath != test.wantPath {
				t.Errorf("handler saw path %q, want %q", seenPath, test.wantPath)
			}
			if got := recorder.Header().Get("Location"); got != test.wantLocation {
				t.Errorf("Location = %q, want %q", got, test.wantLocation)
			}
		})
	}
}

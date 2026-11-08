// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestSEC15_RequestBodiesAreValidated(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	browser := server.newBrowser()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "refuses a body that isn't JSON", body: "username=admin", wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "refuses unknown fields", body: `{"username":"a","password":"b","admin":true}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "refuses a second JSON value", body: `{"username":"a","password":"b"}{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "refuses a field of the wrong type", body: `{"username":1,"password":"b"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "refuses a body over 64 KiB", body: `{"username":"` + strings.Repeat("a", 70_000) + `"}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "body_too_large"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.server.URL+"/api/auth/login", strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Origin", server.publicOrigin)
			expect(t, browser.send(request), test.wantStatus, test.wantCode)
		})
	}
}

func TestSEC12_CrossSiteRequestsAreRefusedOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()

	for _, origin := range []string{"", "https://evil.example.com"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.server.URL+"/api/auth/logout", nil)
		if err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		expect(t, admin.browser.send(request), http.StatusForbidden, "forbidden_origin")
	}

	// The session survived both attempts.
	expect(t, admin.browser.do(http.MethodGet, "/api/auth/session", nil), http.StatusOK, "")
}

func TestUnknownRoutesOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	browser := server.newBrowser()

	expect(t, browser.do(http.MethodGet, "/api/nothing", nil), http.StatusNotFound, "not_found")
	got := browser.do(http.MethodGet, "/some/page", nil)
	if got.status != http.StatusOK || string(got.body) != "web ui" {
		t.Errorf("GET /some/page = %d %q, want the web UI", got.status, got.body)
	}
}

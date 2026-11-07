// SPDX-License-Identifier: Apache-2.0

package middleware_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/middleware"
)

func TestSEC12_StateChangingRequestsFromOtherSitesAreRefused(t *testing.T) {
	publicURL := &url.URL{Scheme: "https", Host: "sentinel.example.com"}

	tests := []struct {
		name       string
		method     string
		origin     string
		wantStatus int
	}{
		{name: "allows a POST from the server's own origin", method: http.MethodPost, origin: "https://sentinel.example.com", wantStatus: http.StatusOK},
		{name: "allows the origin with an explicit default port", method: http.MethodPost, origin: "https://sentinel.example.com:443", wantStatus: http.StatusOK},
		{name: "compares the host case-insensitively", method: http.MethodPost, origin: "https://Sentinel.Example.com", wantStatus: http.StatusOK},
		{name: "refuses a POST from another site", method: http.MethodPost, origin: "https://evil.example.com", wantStatus: http.StatusForbidden},
		{name: "refuses a POST without an Origin header", method: http.MethodPost, origin: "", wantStatus: http.StatusForbidden},
		{name: "refuses a POST from an opaque origin", method: http.MethodPost, origin: "null", wantStatus: http.StatusForbidden},
		{name: "refuses the same host over plain HTTP", method: http.MethodPost, origin: "http://sentinel.example.com", wantStatus: http.StatusForbidden},
		{name: "refuses the same host on another port", method: http.MethodPost, origin: "https://sentinel.example.com:8443", wantStatus: http.StatusForbidden},
		{name: "refuses a malformed Origin header", method: http.MethodPost, origin: "https://%zz", wantStatus: http.StatusForbidden},
		{name: "refuses an Origin header without a host", method: http.MethodPost, origin: "sentinel.example.com", wantStatus: http.StatusForbidden},
		{name: "checks PUT", method: http.MethodPut, origin: "https://evil.example.com", wantStatus: http.StatusForbidden},
		{name: "checks PATCH", method: http.MethodPatch, origin: "https://evil.example.com", wantStatus: http.StatusForbidden},
		{name: "checks DELETE", method: http.MethodDelete, origin: "https://evil.example.com", wantStatus: http.StatusForbidden},
		{name: "allows a GET from another site because it changes nothing", method: http.MethodGet, origin: "https://evil.example.com", wantStatus: http.StatusOK},
		{name: "allows a HEAD without an Origin header", method: http.MethodHead, origin: "", wantStatus: http.StatusOK},
		{name: "allows OPTIONS without an Origin header", method: http.MethodOptions, origin: "", wantStatus: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := serveWithOrigin(t, publicURL, test.method, test.origin, nil)

			if recorder.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if test.wantStatus == http.StatusForbidden && !strings.Contains(recorder.Body.String(), `"forbidden_origin"`) {
				t.Errorf("body = %s, want a forbidden_origin error", recorder.Body.String())
			}
		})
	}
}

func TestSEC13_WebSocketUpgradesCheckTheOrigin(t *testing.T) {
	publicURL := &url.URL{Scheme: "https", Host: "sentinel.example.com"}
	upgrade := map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}

	tests := []struct {
		name       string
		origin     string
		wantStatus int
	}{
		{name: "allows an upgrade from the server's own origin", origin: "https://sentinel.example.com", wantStatus: http.StatusOK},
		{name: "refuses an upgrade from another site", origin: "https://evil.example.com", wantStatus: http.StatusForbidden},
		{name: "refuses an upgrade without an Origin header", origin: "", wantStatus: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := serveWithOrigin(t, publicURL, http.MethodGet, test.origin, upgrade)

			if recorder.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
		})
	}
}

func TestRequireSameOriginWithoutPublicURL(t *testing.T) {
	tests := []struct {
		name       string
		requestURL string
		useTLS     bool
		origin     string
		wantStatus int
	}{
		{name: "accepts the origin the request was sent to over HTTPS", requestURL: "https://10.0.0.5:7443/", useTLS: true, origin: "https://10.0.0.5:7443", wantStatus: http.StatusOK},
		{name: "accepts the origin the request was sent to over HTTP", requestURL: "http://127.0.0.1:7443/", origin: "http://127.0.0.1:7443", wantStatus: http.StatusOK},
		{name: "refuses another site", requestURL: "https://10.0.0.5:7443/", useTLS: true, origin: "https://evil.example.com", wantStatus: http.StatusForbidden},
		{name: "refuses an HTTP origin for an HTTPS request", requestURL: "https://10.0.0.5:7443/", useTLS: true, origin: "http://10.0.0.5:7443", wantStatus: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := middleware.WithClientInfo(
				middleware.RequireSameOrigin(okHandler(), nil), nil)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, test.requestURL, nil)
			request.Header.Set("Origin", test.origin)
			if test.useTLS {
				request.TLS = &tls.ConnectionState{}
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
		})
	}
}

func serveWithOrigin(t *testing.T, publicURL *url.URL, method, origin string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	handler := middleware.RequireSameOrigin(okHandler(), publicURL)
	request := httptest.NewRequestWithContext(t.Context(), method, "https://sentinel.example.com/api/thing", nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

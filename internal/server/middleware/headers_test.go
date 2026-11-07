// SPDX-License-Identifier: Apache-2.0

package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/middleware"
)

func TestSEC11_SecurityHeadersAreOnEveryResponse(t *testing.T) {
	handler := middleware.SecurityHeaders(http.NotFoundHandler(), middleware.SecurityHeadersOptions{})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing", nil))

	headers := recorder.Header()
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "forbids framing", header: "X-Frame-Options", want: "DENY"},
		{name: "sends no referrer", header: "Referrer-Policy", want: "no-referrer"},
		{name: "stops content sniffing", header: "X-Content-Type-Options", want: "nosniff"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := headers.Get(test.header); got != test.want {
				t.Errorf("%s = %q, want %q", test.header, got, test.want)
			}
		})
	}

	t.Run("sends a strict content security policy", func(t *testing.T) {
		policy := headers.Get("Content-Security-Policy")
		for _, directive := range []string{
			"default-src 'self'", "script-src 'self'", "object-src 'none'", "frame-ancestors 'none'",
		} {
			if !strings.Contains(policy, directive) {
				t.Errorf("CSP %q is missing %q", policy, directive)
			}
		}
		if strings.Contains(policy, "unsafe-inline") || strings.Contains(policy, "unsafe-eval") {
			t.Errorf("CSP %q allows inline or eval'd code", policy)
		}
		if strings.Contains(policy, "http") {
			t.Errorf("CSP %q allows a third-party origin", policy)
		}
	})

	t.Run("does not send HSTS unless asked", func(t *testing.T) {
		if got := headers.Get("Strict-Transport-Security"); got != "" {
			t.Errorf("Strict-Transport-Security = %q, want none", got)
		}
	})
}

func TestSecurityHeadersHSTS(t *testing.T) {
	t.Run("sends HSTS when the certificate is trusted", func(t *testing.T) {
		handler := middleware.SecurityHeaders(http.NotFoundHandler(),
			middleware.SecurityHeadersOptions{StrictTransportSecurity: true})
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

		if got := recorder.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
			t.Errorf("Strict-Transport-Security = %q, want max-age=31536000", got)
		}
	})
}

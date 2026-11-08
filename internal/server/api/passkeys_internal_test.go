// SPDX-License-Identifier: Apache-2.0

package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/middleware"
)

func TestRelyingParty(t *testing.T) {
	tests := []struct {
		name      string
		publicURL *url.URL
		target    string
		useTLS    bool
		want      auth.RelyingParty
	}{
		{
			name:      "uses the public URL when configured",
			publicURL: &url.URL{Scheme: "https", Host: "sentinel.example.com"},
			target:    "https://10.0.0.5:7443/",
			useTLS:    true,
			want:      auth.RelyingParty{ID: "sentinel.example.com", Origin: "https://sentinel.example.com"},
		},
		{
			name:   "uses the requested host and port otherwise",
			target: "https://sentinel.lan:7443/",
			useTLS: true,
			want:   auth.RelyingParty{ID: "sentinel.lan", Origin: "https://sentinel.lan:7443"},
		},
		{
			name:   "handles a host without a port over plain HTTP",
			target: "http://localhost/",
			want:   auth.RelyingParty{ID: "localhost", Origin: "http://localhost"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := &handlers{publicURL: test.publicURL}
			var got auth.RelyingParty
			handler := middleware.WithClientInfo(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = h.relyingParty(r)
			}), nil)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.target, nil)
			if test.useTLS {
				request.TLS = &tls.ConnectionState{}
			}

			handler.ServeHTTP(httptest.NewRecorder(), request)

			if got != test.want {
				t.Errorf("relyingParty = %+v, want %+v", got, test.want)
			}
		})
	}
}

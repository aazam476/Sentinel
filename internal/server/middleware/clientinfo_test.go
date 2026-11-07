// SPDX-License-Identifier: Apache-2.0

package middleware_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/aazam476/sentinel/internal/server/middleware"
)

func TestSEC14_ForwardedHeadersAreOnlyBelievedFromTrustedProxies(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}

	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		useTLS     bool
		wantIP     string
		wantHTTPS  bool
		wantHost   string
	}{
		{
			name:       "uses the connection address for a direct client",
			remoteAddr: "203.0.113.7:51000",
			wantIP:     "203.0.113.7",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "ignores forwarded headers from an untrusted client",
			remoteAddr: "203.0.113.7:51000",
			headers: map[string]string{
				"X-Forwarded-For":   "198.51.100.1",
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "evil.example.com",
			},
			wantIP:   "203.0.113.7",
			wantHost: "sentinel.example.com",
		},
		{
			name:       "believes forwarded headers from a trusted proxy",
			remoteAddr: "10.0.0.2:40000",
			headers: map[string]string{
				"X-Forwarded-For":   "198.51.100.1",
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "public.example.com",
			},
			wantIP:    "198.51.100.1",
			wantHTTPS: true,
			wantHost:  "public.example.com",
		},
		{
			name:       "skips trusted proxies in a chain to find the real client",
			remoteAddr: "10.0.0.2:40000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.1, 10.0.0.9"},
			wantIP:     "198.51.100.1",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "does not believe an address the client put at the start of the chain",
			remoteAddr: "10.0.0.2:40000",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4, 198.51.100.1"},
			wantIP:     "198.51.100.1",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "uses the leftmost address when every hop is a trusted proxy",
			remoteAddr: "10.0.0.2:40000",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.5, 10.0.0.9"},
			wantIP:     "10.0.0.5",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "stops at a malformed hop and keeps the last trustworthy address",
			remoteAddr: "10.0.0.2:40000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.1, not-an-ip"},
			wantIP:     "10.0.0.2",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "uses the proxy address when it sends no X-Forwarded-For",
			remoteAddr: "10.0.0.2:40000",
			wantIP:     "10.0.0.2",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "lets a trusted proxy say the client used plain HTTP",
			remoteAddr: "10.0.0.2:40000",
			headers:    map[string]string{"X-Forwarded-Proto": "http"},
			useTLS:     true,
			wantIP:     "10.0.0.2",
			wantHTTPS:  false,
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "reports HTTPS for a direct TLS connection",
			remoteAddr: "203.0.113.7:51000",
			useTLS:     true,
			wantIP:     "203.0.113.7",
			wantHTTPS:  true,
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "understands IPv6 proxies and clients",
			remoteAddr: "[fd00::2]:40000",
			headers:    map[string]string{"X-Forwarded-For": "2001:db8::1"},
			wantIP:     "2001:db8::1",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "treats an IPv4-mapped IPv6 connection as IPv4",
			remoteAddr: "[::ffff:10.0.0.2]:40000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.1"},
			wantIP:     "198.51.100.1",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "handles a remote address without a port",
			remoteAddr: "203.0.113.7",
			wantIP:     "203.0.113.7",
			wantHost:   "sentinel.example.com",
		},
		{
			name:       "returns no IP for a remote address that is not an IP",
			remoteAddr: "pipe",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.1"},
			wantIP:     "invalid IP",
			wantHost:   "sentinel.example.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got middleware.ClientInfo
			handler := middleware.WithClientInfo(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = middleware.ClientInfoFrom(r.Context())
			}), trusted)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://sentinel.example.com/", nil)
			request.RemoteAddr = test.remoteAddr
			for name, value := range test.headers {
				request.Header.Set(name, value)
			}
			if test.useTLS {
				request.TLS = &tls.ConnectionState{}
			}

			handler.ServeHTTP(httptest.NewRecorder(), request)

			if got.IP.String() != test.wantIP {
				t.Errorf("IP = %s, want %s", got.IP, test.wantIP)
			}
			if got.IsHTTPS != test.wantHTTPS {
				t.Errorf("IsHTTPS = %v, want %v", got.IsHTTPS, test.wantHTTPS)
			}
			if got.Host != test.wantHost {
				t.Errorf("Host = %q, want %q", got.Host, test.wantHost)
			}
		})
	}
}

func TestClientInfoFrom(t *testing.T) {
	t.Run("returns a zero value when no middleware stored one", func(t *testing.T) {
		info := middleware.ClientInfoFrom(t.Context())
		if info.IP.IsValid() || info.IsHTTPS || info.Host != "" {
			t.Errorf("ClientInfoFrom = %+v, want the zero value", info)
		}
	})
}

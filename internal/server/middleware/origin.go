// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// RequireSameOrigin refuses requests that a different website could have
// made the user's browser send (SEC-12, SEC-13).
//
// It checks every state-changing request (anything but GET, HEAD and
// OPTIONS) and every WebSocket upgrade. Browsers always send an Origin header
// on those, and a page on another site can't fake it, so requiring Origin to
// match the server's own origin stops cross-site request forgery. Together
// with SameSite=Strict session cookies this is the CSRF protection; see
// docs/decisions/0008-csrf-protection.md.
//
// The server's origin is publicURL when configured. Without one it is the
// scheme and host the request was sent to, which needs WithClientInfo to run
// first.
func RequireSameOrigin(next http.Handler, publicURL *url.URL) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !needsOriginCheck(r) {
			next.ServeHTTP(w, r)
			return
		}

		expected := expectedOrigin(r, publicURL)
		if !sameOrigin(r.Header.Get("Origin"), expected) {
			writeJSONError(w, http.StatusForbidden, "forbidden_origin",
				"This request came from a different website and was refused.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func needsOriginCheck(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return isWebSocketUpgrade(r)
	default:
		return true
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func expectedOrigin(r *http.Request, publicURL *url.URL) *url.URL {
	if publicURL != nil {
		return publicURL
	}
	info := ClientInfoFrom(r.Context())
	scheme := "http"
	if info.IsHTTPS {
		scheme = "https"
	}
	return &url.URL{Scheme: scheme, Host: info.Host}
}

// sameOrigin compares an Origin header with the expected origin. Scheme and
// host are case-insensitive, and an explicit default port (":443" for https,
// ":80" for http) is the same as no port.
func sameOrigin(originHeader string, expected *url.URL) bool {
	if originHeader == "" || originHeader == "null" {
		return false
	}
	origin, err := url.Parse(originHeader)
	if err != nil || origin.Host == "" {
		return false
	}
	return strings.EqualFold(origin.Scheme, expected.Scheme) &&
		hostWithPort(origin) == hostWithPort(expected)
}

func hostWithPort(origin *url.URL) string {
	port := origin.Port()
	if port == "" {
		port = defaultPort(strings.ToLower(origin.Scheme))
	}
	return strings.ToLower(origin.Hostname()) + ":" + port
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"net/http"
	"strings"
)

// WithBasePath serves next under basePath, so a reverse proxy can host
// Sentinel at, for example, https://tools.example.com/sentinel/ (SEC-14).
//
// Requests under basePath reach next with the prefix removed, so the rest of
// the server never needs to know about it. A request for basePath itself is
// redirected to basePath + "/", and anything outside basePath gets 404.
// basePath must already be normalized by the config package.
func WithBasePath(next http.Handler, basePath string) http.Handler {
	if basePath == "/" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == basePath {
			http.Redirect(w, r, basePath+"/", http.StatusPermanentRedirect)
			return
		}

		rest, found := strings.CutPrefix(r.URL.Path, basePath+"/")
		if !found {
			http.NotFound(w, r)
			return
		}

		stripped := r.Clone(r.Context())
		stripped.URL.Path = "/" + rest
		stripped.URL.RawPath = ""
		next.ServeHTTP(w, stripped)
	})
}

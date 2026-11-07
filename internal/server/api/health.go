// SPDX-License-Identifier: Apache-2.0

package api

import (
	"io"
	"net/http"
)

// handleHealthz answers "ok" so reverse proxies and monitoring tools can
// check that the server is up. It needs no login and reveals nothing else.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, errorCodeMethodNotAllowed, "Use GET.")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// The status code already reached the client; a failed body write has
	// nowhere else to be reported.
	_, _ = io.WriteString(w, "ok")
}

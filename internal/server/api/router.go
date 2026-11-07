// SPDX-License-Identifier: Apache-2.0

package api

import "net/http"

// RouterOptions holds what NewRouter needs to build the routes.
type RouterOptions struct {
	// WebUI serves the browser UI for every path that isn't an API route.
	WebUI http.Handler
}

// NewRouter returns the handler for every URL the server answers.
func NewRouter(options RouterOptions) http.Handler {
	mux := http.NewServeMux()
	// Without a method in the pattern, so a POST gets 405 here instead of
	// falling through to the web UI's "/" route.
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/api/", handleUnknownAPIRoute)
	mux.Handle("/", options.WebUI)
	return mux
}

// handleUnknownAPIRoute answers API paths that don't exist with a JSON 404,
// rather than letting them fall through to the web UI's HTML page.
func handleUnknownAPIRoute(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, errorCodeNotFound, "There is no API endpoint at this address.")
}

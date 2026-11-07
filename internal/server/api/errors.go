// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net/http"
)

// Error codes the API returns in {"error": {"code": ...}}. The web UI
// switches on these, so they never change once released.
const (
	errorCodeNotFound         = "not_found"
	errorCodeMethodNotAllowed = "method_not_allowed"
)

// errorResponse is the JSON body of every failed API request.
type errorResponse struct {
	Error errorDetail `json:"error"`
}

// errorDetail says what went wrong: a stable code for programs and a
// sentence for people.
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError sends an API error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

// writeJSON sends value as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// The status code already reached the client; a failed body write has
	// nowhere else to be reported.
	_ = json.NewEncoder(w).Encode(value)
}

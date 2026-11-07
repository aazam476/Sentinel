// SPDX-License-Identifier: Apache-2.0

package middleware

import (
	"encoding/json"
	"net/http"
)

// writeJSONError sends an error in the same shape the API uses:
// {"error": {"code": "...", "message": "..."}}.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	body := map[string]map[string]string{"error": {"code": code, "message": message}}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status code already reached the client; a failed body write has
	// nowhere else to be reported.
	_ = json.NewEncoder(w).Encode(body)
}

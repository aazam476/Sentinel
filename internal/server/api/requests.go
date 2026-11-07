// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/middleware"
)

// maxRequestBody is the largest JSON body the API reads. Every request is a
// small form; anything bigger is a mistake or an attack (SEC-15).
const maxRequestBody = 64 * 1024

// decodeJSON reads the request body into destination. It rejects bodies over
// maxRequestBody, unknown fields and anything after the JSON value, and
// writes the error response itself, returning false, when the body is bad.
func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	decoder.DisallowUnknownFields()

	err := decoder.Decode(destination)
	if err == nil && decoder.Decode(&struct{}{}) != io.EOF {
		err = errors.New("unexpected data after the JSON value")
	}
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, errorCodeBodyTooLarge, "The request is too large.")
		return false
	}
	writeError(w, http.StatusBadRequest, errorCodeInvalidJSON, "The request body isn't valid JSON for this endpoint.")
	return false
}

// requestInfo describes the client, for the audit log and rate limiting.
func requestInfo(r *http.Request) auth.RequestInfo {
	return auth.RequestInfo{
		IP:        middleware.ClientInfoFrom(r.Context()).IP.String(),
		UserAgent: r.UserAgent(),
	}
}

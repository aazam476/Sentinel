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

// maxRequestBody is the largest JSON body the API reads, except for the few
// requests that carry a file's text (decodeText). Every other request is a
// small form; anything bigger is a mistake or an attack (SEC-15).
const maxRequestBody = 64 * 1024

// jsonGrowth is the most bytes JSON takes for one byte of text: it writes
// a control character as six, such as \u0001.
const jsonGrowth = 6

// decodeJSON reads the request body into destination. It rejects bodies over
// maxRequestBody, unknown fields and anything after the JSON value, and
// writes the error response itself, returning false, when the body is bad.
func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	return decodeBody(w, r, maxRequestBody, destination)
}

// decodeText is decodeJSON for a request that carries the text of a file:
// a script, a stack's files, a text file being saved. textBytes is the most
// text such a request may hold. The body may be as large as that much text
// can be as JSON, on top of the usual room for the rest of the request; the
// text's own length is still for the handler, or what it calls, to check.
func decodeText(w http.ResponseWriter, r *http.Request, textBytes int, destination any) bool {
	return decodeBody(w, r, int64(textBytes)*jsonGrowth+maxRequestBody, destination)
}

// decodeBody reads a JSON body of at most limit bytes into destination.
func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
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

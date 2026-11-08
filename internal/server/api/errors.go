// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// Error codes the API returns in {"error": {"code": ...}}. The web UI
// switches on these, so they never change once released.
const (
	errorCodeNotFound           = "not_found"
	errorCodeMethodNotAllowed   = "method_not_allowed"
	errorCodeInvalidJSON        = "invalid_json"
	errorCodeBodyTooLarge       = "body_too_large"
	errorCodeInvalidInput       = "invalid_input"
	errorCodeInvalidCredentials = "invalid_credentials" //nolint:gosec // G101: an error code, not a credential
	errorCodeInvalidCode        = "invalid_code"
	errorCodeInvalidSetupToken  = "invalid_setup_token"
	errorCodeSetupNotAvailable  = "setup_not_available"
	errorCodeSetupExpired       = "setup_expired"
	errorCodeNotAuthenticated   = "not_authenticated"
	errorCodeWrongLoginStep     = "wrong_login_step"
	errorCodeStepUpRequired     = "step_up_required"
	errorCodeForbidden          = "forbidden"
	errorCodeUsernameTaken      = "username_taken"
	errorCodeLastAdmin          = "last_admin"
	errorCodeTooManyAttempts    = "too_many_attempts"
	errorCodeInternal           = "internal"
	errorCodePasskeyFailed      = "passkey_failed"
	errorCodeLastSecondFactor   = "last_second_factor"
)

// ErrorResponse is the JSON body of every failed API request.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail says what went wrong: a stable code for programs, a sentence
// for people, and for invalid input the field it is about.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	// RetryAfterSeconds is set for too_many_attempts.
	RetryAfterSeconds int `json:"retryAfterSeconds,omitempty"`
}

// writeError sends an API error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Error: ErrorDetail{Code: code, Message: message}})
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

// writeInvalidInput reports a bad query parameter or field.
func writeInvalidInput(w http.ResponseWriter, field, message string) {
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: ErrorDetail{Code: errorCodeInvalidInput, Message: message, Field: field}})
}

// knownError maps an error from the auth package to its HTTP status and
// code.
type knownError struct {
	err    error
	status int
	code   string
}

// knownErrors lists the auth errors and how the API reports them.
func knownErrors() []knownError {
	return []knownError{
		{auth.ErrInvalidCredentials, http.StatusUnauthorized, errorCodeInvalidCredentials},
		{auth.ErrInvalidCode, http.StatusUnauthorized, errorCodeInvalidCode},
		{auth.ErrInvalidSetupToken, http.StatusForbidden, errorCodeInvalidSetupToken},
		{auth.ErrSetupNotAvailable, http.StatusConflict, errorCodeSetupNotAvailable},
		{auth.ErrSetupExpired, http.StatusGone, errorCodeSetupExpired},
		{auth.ErrNotAuthenticated, http.StatusUnauthorized, errorCodeNotAuthenticated},
		{auth.ErrWrongLoginStep, http.StatusConflict, errorCodeWrongLoginStep},
		{auth.ErrStepUpRequired, http.StatusForbidden, errorCodeStepUpRequired},
		{auth.ErrForbidden, http.StatusForbidden, errorCodeForbidden},
		{auth.ErrNotFound, http.StatusNotFound, errorCodeNotFound},
		{auth.ErrUsernameTaken, http.StatusConflict, errorCodeUsernameTaken},
		{auth.ErrLastAdmin, http.StatusConflict, errorCodeLastAdmin},
		{auth.ErrPasskeyFailed, http.StatusUnauthorized, errorCodePasskeyFailed},
		{auth.ErrLastSecondFactor, http.StatusConflict, errorCodeLastSecondFactor},
	}
}

// writeServiceError turns an error from a service into a response. Unknown
// errors become a 500 with a generic message, and the real error is logged
// for the operator: its text may contain internal details.
func writeServiceError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var validation *auth.ValidationError
	if errors.As(err, &validation) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: ErrorDetail{
			Code: errorCodeInvalidInput, Message: validation.Message, Field: validation.Field,
		}})
		return
	}

	var tooMany *auth.TooManyAttemptsError
	if errors.As(err, &tooMany) {
		seconds := int(math.Ceil(tooMany.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: ErrorDetail{
			Code: errorCodeTooManyAttempts, Message: tooMany.Error(), RetryAfterSeconds: seconds,
		}})
		return
	}

	for _, known := range knownErrors() {
		if errors.Is(err, known.err) {
			writeError(w, known.status, known.code, known.err.Error())
			return
		}
	}

	logger.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, errorCodeInternal, "Something went wrong on the server. The details are in its log.")
}

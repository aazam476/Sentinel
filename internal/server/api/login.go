// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// Session states the web UI routes on.
const (
	sessionStateSignedIn           = "signed_in"
	sessionStateSecondFactor       = "second_factor"
	sessionStateEnrollSecondFactor = "enroll_second_factor"
)

// LoginRequest is the password step of signing in.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse says what the browser must do next: "second_factor" or
// "enroll_second_factor".
type LoginResponse struct {
	NextStep string `json:"nextStep"`
}

// SecondFactorRequest finishes signing in with one of the two codes.
type SecondFactorRequest struct {
	TOTPCode     string `json:"totpCode,omitempty"`
	RecoveryCode string `json:"recoveryCode,omitempty"`
}

// TOTPCodeRequest carries a TOTP code, for enrollment and step-up.
type TOTPCodeRequest struct {
	TOTPCode string `json:"totpCode"`
}

// SessionResponse describes the current session.
type SessionResponse struct {
	// State is "signed_in", "second_factor" or "enroll_second_factor".
	State string       `json:"state"`
	User  UserIdentity `json:"user"`
	// StepUpValidUntil is when the current step-up window ends (Unix
	// milliseconds), for signed-in sessions.
	StepUpValidUntil *int64 `json:"stepUpValidUntil" tstype:"number | null,required"`
}

// UserIdentity is who the session belongs to.
type UserIdentity struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// SignedInResponse is returned when a login, setup or enrollment finishes.
type SignedInResponse struct {
	Session SessionResponse `json:"session"`
	// RecoveryCodes are shown once, after setup or enrollment.
	RecoveryCodes []string `json:"recoveryCodes"`
	// RemainingRecoveryCodes is set after signing in with a recovery code.
	RemainingRecoveryCodes *int `json:"remainingRecoveryCodes" tstype:"number | null,required"`
}

func (h *handlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := h.auth.Login(r.Context(), auth.LoginInput{Username: request.Username, Password: request.Password}, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.setSessionCookie(w, result.SessionToken)
	writeJSON(w, http.StatusOK, LoginResponse{NextStep: string(result.NextStep)})
}

func (h *handlers) handleSecondFactor(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request SecondFactorRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	signedIn, err := h.auth.VerifySecondFactor(r.Context(), session, auth.SecondFactorInput{
		TOTPCode: request.TOTPCode, RecoveryCode: request.RecoveryCode,
	}, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.respondSignedIn(w, r, signedIn)
}

func (h *handlers) handleBeginEnrollment(w http.ResponseWriter, r *http.Request, session auth.Session) {
	enrollment, err := h.auth.BeginTOTPEnrollment(r.Context(), session)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, enrollmentResponse(enrollment))
}

func (h *handlers) handleConfirmEnrollment(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request TOTPCodeRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	signedIn, err := h.auth.ConfirmTOTPEnrollment(r.Context(), session, request.TOTPCode, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.respondSignedIn(w, r, signedIn)
}

func (h *handlers) handleStepUp(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request TOTPCodeRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.auth.StepUp(r.Context(), session, request.TOTPCode, requestInfo(r)); err != nil {
		h.writeError(w, err)
		return
	}
	h.handleSession(w, r, session)
}

func (h *handlers) handleLogout(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if err := h.auth.Logout(r.Context(), session, requestInfo(r)); err != nil {
		h.writeError(w, err)
		return
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleSession describes the current session. It re-reads the session so
// a step-up just done is reflected.
func (h *handlers) handleSession(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if fresh, err := h.auth.Authenticate(r.Context(), cookie.Value); err == nil {
			session = fresh
		}
	}
	writeJSON(w, http.StatusOK, sessionResponse(session))
}

// respondSignedIn sets the new session cookie and describes the session.
func (h *handlers) respondSignedIn(w http.ResponseWriter, r *http.Request, signedIn auth.SignedIn) {
	session, err := h.auth.Authenticate(r.Context(), signedIn.SessionToken)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.setSessionCookie(w, signedIn.SessionToken)

	response := SignedInResponse{Session: sessionResponse(session), RecoveryCodes: signedIn.RecoveryCodes}
	if response.RecoveryCodes == nil {
		response.RecoveryCodes = []string{}
	}
	if signedIn.UsedRecoveryCode {
		remaining := signedIn.RemainingRecoveryCodes
		response.RemainingRecoveryCodes = &remaining
	}
	writeJSON(w, http.StatusOK, response)
}

func sessionResponse(session auth.Session) SessionResponse {
	response := SessionResponse{
		State: sessionStateSignedIn,
		User:  UserIdentity{ID: session.UserID, Username: session.Username, Role: string(session.Role)},
	}
	switch {
	case !session.IsSignedIn() && session.HasSecondFactor:
		response.State = sessionStateSecondFactor
	case !session.IsSignedIn():
		response.State = sessionStateEnrollSecondFactor
	default:
		validUntil := *session.SecondFactorAt + auth.StepUpWindow.Milliseconds()
		response.StepUpValidUntil = &validUntil
	}
	return response
}

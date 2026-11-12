// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// SetupStatusResponse says whether the first admin still has to be created.
type SetupStatusResponse struct {
	SetupRequired bool `json:"setupRequired"`
}

// BeginSetupRequest is the first setup step (SEC-02).
type BeginSetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// TOTPEnrollmentResponse lets a user add Sentinel to an authenticator app.
type TOTPEnrollmentResponse struct {
	// Secret is for typing in by hand.
	Secret string `json:"secret"`
	// URL is the otpauth:// URL the QR code encodes.
	URL string `json:"url"`
	// QRCode is a PNG data: URL.
	QRCode string `json:"qrCode"`
}

// BeginSetupResponse carries the TOTP secret for the new admin.
type BeginSetupResponse struct {
	SetupID string                 `json:"setupId"`
	TOTP    TOTPEnrollmentResponse `json:"totp"`
}

// CompleteSetupRequest proves the authenticator app works, or skips 2FA
// for now (ADR 0039).
type CompleteSetupRequest struct {
	SetupID  string `json:"setupId"`
	TOTPCode string `json:"totpCode"`
	// SkipTwoFactor creates the admin without 2FA; TOTPCode is ignored.
	SkipTwoFactor bool `json:"skipTwoFactor,omitempty"`
}

func (h *handlers) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := h.auth.SetupRequired(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SetupStatusResponse{SetupRequired: required})
}

func (h *handlers) handleBeginSetup(w http.ResponseWriter, r *http.Request) {
	var request BeginSetupRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := h.auth.BeginSetup(r.Context(), auth.BeginSetupInput{
		Username: request.Username, Password: request.Password,
	}, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, BeginSetupResponse{SetupID: result.SetupID, TOTP: enrollmentResponse(result.TOTP)})
}

func (h *handlers) handleCompleteSetup(w http.ResponseWriter, r *http.Request) {
	var request CompleteSetupRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	signedIn, err := h.auth.CompleteSetup(r.Context(), auth.CompleteSetupInput{
		SetupID: request.SetupID, TOTPCode: request.TOTPCode, SkipTwoFactor: request.SkipTwoFactor,
	}, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.respondSignedIn(w, r, signedIn)
}

func enrollmentResponse(enrollment auth.TOTPEnrollment) TOTPEnrollmentResponse {
	return TOTPEnrollmentResponse{Secret: enrollment.Secret, URL: enrollment.URL, QRCode: enrollment.QRCode}
}

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// SecuritySettings are the server-wide sign-in rules (ADR 0039).
type SecuritySettings struct {
	// RequireTwoFactor makes every user set up TOTP or a passkey. Off,
	// two-factor authentication is each user's choice.
	RequireTwoFactor bool `json:"requireTwoFactor"`
}

// handleGetSecuritySettings is GET /api/settings/security, for every
// signed-in user: the account page needs to know whether 2FA can be
// removed.
func (h *handlers) handleGetSecuritySettings(w http.ResponseWriter, r *http.Request, _ auth.Session) {
	required, err := h.auth.TwoFactorRequired(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SecuritySettings{RequireTwoFactor: required})
}

// handleUpdateSecuritySettings is PUT /api/settings/security, for admins
// after a recent step-up (SEC-07).
func (h *handlers) handleUpdateSecuritySettings(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request SecuritySettings
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := h.auth.SetTwoFactorRequired(r.Context(), session, request.RequireTwoFactor, requestInfo(r)); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, request)
}

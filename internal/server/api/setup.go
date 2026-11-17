// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/middleware"

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

// setupAllowedHere refuses first-time setup from where it could be a
// takeover (ADR 0055): from outside setup_allowed_from (by default, this
// machine and private networks), or, without public_url, through a
// browser that reached the server by a public name, which is how a DNS
// rebinding page would reach it from someone's LAN.
func (h *handlers) setupAllowedHere(w http.ResponseWriter, r *http.Request) bool {
	client := middleware.ClientInfoFrom(r.Context())
	networks := h.config.SetupAllowedFrom
	if len(networks) == 0 {
		networks = config.DefaultSetupNetworks()
	}
	if !slices.ContainsFunc(networks, func(network netip.Prefix) bool { return network.Contains(client.IP.Unmap()) }) {
		writeError(w, http.StatusForbidden, errorCodeSetupNotHere, "First-time setup can only be done from this machine or a private network. "+
			"Open Sentinel from there, or list your address under setup_allowed_from in server.toml.")
		return false
	}
	if h.config.PublicURL == nil && !isLocalHost(client.Host) {
		writeError(w, http.StatusForbidden, errorCodeSetupNotHere, "Open Sentinel by its IP address or a local name to set it up, "+
			"or set public_url in server.toml to the address you use.")
		return false
	}
	return true
}

// isLocalHost says whether a Host header names this machine by address,
// "localhost" or a name that only resolves locally.
func isLocalHost(hostPort string) bool {
	host := hostPort
	if split, _, err := net.SplitHostPort(hostPort); err == nil {
		host = split
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	if host == "localhost" || (host != "" && !strings.Contains(host, ".")) {
		return true
	}
	for _, suffix := range []string{".local", ".lan", ".home.arpa", ".internal", ".localhost"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func (h *handlers) handleBeginSetup(w http.ResponseWriter, r *http.Request) {
	if !h.setupAllowedHere(w, r) {
		return
	}
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
	if !h.setupAllowedHere(w, r) {
		return
	}
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

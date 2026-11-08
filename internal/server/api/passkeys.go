// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/middleware"
)

// PasskeyOptionsResponse carries the options for the browser's
// navigator.credentials.create() or .get() call, with binary values as
// base64url strings.
type PasskeyOptionsResponse struct {
	Options json.RawMessage `json:"options" tstype:"{ [key: string]: unknown }"`
}

// FinishPasskeyRequest returns the browser's answer.
type FinishPasskeyRequest struct {
	// Name labels a new passkey, e.g. "Laptop". Ignored when signing in.
	Name string `json:"name,omitempty"`
	// Credential is the PublicKeyCredential from the browser, serialized
	// with its binary values as base64url strings.
	Credential json.RawMessage `json:"credential" tstype:"{ [key: string]: unknown }"`
}

// PasskeySummary is one of the user's passkeys.
type PasskeySummary struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt *int64 `json:"lastUsedAt" tstype:"number | null,required"`
}

// PasskeysResponse lists the user's passkeys.
type PasskeysResponse struct {
	Passkeys []PasskeySummary `json:"passkeys"`
}

// relyingParty works out which site passkeys belong to: the public URL's
// host when configured, otherwise the host the browser asked for.
func (h *handlers) relyingParty(r *http.Request) auth.RelyingParty {
	if h.publicURL != nil {
		return auth.RelyingParty{ID: h.publicURL.Hostname(), Origin: h.publicURL.Scheme + "://" + h.publicURL.Host}
	}
	info := middleware.ClientInfoFrom(r.Context())
	scheme := "http"
	if info.IsHTTPS {
		scheme = "https"
	}
	host, _, err := net.SplitHostPort(info.Host)
	if err != nil {
		host = info.Host
	}
	return auth.RelyingParty{ID: host, Origin: scheme + "://" + info.Host}
}

func (h *handlers) handleBeginPasskeyRegistration(w http.ResponseWriter, r *http.Request, session auth.Session) {
	options, err := h.auth.BeginPasskeyRegistration(r.Context(), session, h.relyingParty(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PasskeyOptionsResponse{Options: options})
}

// handleFinishPasskeyRegistration saves the new passkey. For a user whose
// first second factor it is, it also signs them in.
func (h *handlers) handleFinishPasskeyRegistration(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request FinishPasskeyRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	signedIn, err := h.auth.FinishPasskeyRegistration(r.Context(), session, h.relyingParty(r), request.Name, request.Credential, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if signedIn.SessionToken != "" {
		h.respondSignedIn(w, r, signedIn)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) handleBeginPasskeyAssertion(w http.ResponseWriter, r *http.Request, session auth.Session) {
	options, err := h.auth.BeginPasskeyAssertion(r.Context(), session, h.relyingParty(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, PasskeyOptionsResponse{Options: options})
}

// handleFinishPasskeyAssertion finishes signing in, or a step-up check for
// a signed-in session, with a passkey.
func (h *handlers) handleFinishPasskeyAssertion(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request FinishPasskeyRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	signedIn, err := h.auth.FinishPasskeyAssertion(r.Context(), session, h.relyingParty(r), request.Credential, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if signedIn.SessionToken != "" {
		h.respondSignedIn(w, r, signedIn)
		return
	}
	h.handleSession(w, r, session)
}

func (h *handlers) handleListPasskeys(w http.ResponseWriter, r *http.Request, session auth.Session) {
	passkeys, err := h.auth.ListPasskeys(r.Context(), session)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := PasskeysResponse{Passkeys: make([]PasskeySummary, 0, len(passkeys))}
	for _, passkey := range passkeys {
		response.Passkeys = append(response.Passkeys, PasskeySummary(passkey))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleDeletePasskey(w http.ResponseWriter, r *http.Request, session auth.Session) {
	passkeyID, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.auth.DeletePasskey(r.Context(), session, passkeyID, requestInfo(r)); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// EnrollmentToken is an enrollment token as admins see it. Its secret is
// only ever returned once, by CreateEnrollmentTokenResponse.
type EnrollmentToken struct {
	ID        int64  `json:"id"`
	Note      string `json:"note"`
	CreatedBy string `json:"createdBy"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	MaxUses   int    `json:"maxUses"`
	UseCount  int    `json:"useCount"`
	Revoked   bool   `json:"revoked"`
	// Usable is false once the token is revoked, expired or used up.
	Usable bool `json:"usable"`
}

// EnrollmentTokensResponse lists recent tokens, newest first, and the
// fingerprint install commands need.
type EnrollmentTokensResponse struct {
	Tokens            []EnrollmentToken `json:"tokens"`
	ServerFingerprint string            `json:"serverFingerprint"`
	// TLSFingerprint is the server's self-signed certificate key, which
	// install commands pass as --tls-fingerprint; "" when the certificate
	// comes from a real authority (ACME or a TLS proxy).
	TLSFingerprint string `json:"tlsFingerprint"`
}

// CreateEnrollmentTokenRequest describes a new token. Zero values mean the
// defaults: 24 hours and a single use.
type CreateEnrollmentTokenRequest struct {
	Note           string `json:"note"`
	ExpiresInHours int    `json:"expiresInHours"`
	MaxUses        int    `json:"maxUses"`
}

// CreateEnrollmentTokenResponse is a new token with its secret, shown once,
// and the server key fingerprint for the install command (§7.2).
type CreateEnrollmentTokenResponse struct {
	Token             EnrollmentToken `json:"token"`
	Secret            string          `json:"secret"`
	ServerFingerprint string          `json:"serverFingerprint"`
	// TLSFingerprint is as in EnrollmentTokensResponse.
	TLSFingerprint string `json:"tlsFingerprint"`
}

func (h *handlers) handleListEnrollmentTokens(w http.ResponseWriter, r *http.Request, session auth.Session) {
	tokens, err := h.fleet.ListEnrollmentTokens(r.Context(), session)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := EnrollmentTokensResponse{Tokens: make([]EnrollmentToken, 0, len(tokens)), ServerFingerprint: h.serverKey.Fingerprint(), TLSFingerprint: h.tlsPin}
	now := h.clock.Now().UnixMilli()
	for _, token := range tokens {
		response.Tokens = append(response.Tokens, enrollmentToken(token, now))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleCreateEnrollmentToken(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request CreateEnrollmentTokenRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	created, err := h.fleet.CreateEnrollmentToken(r.Context(), session, fleet.CreateTokenInput{
		Note: request.Note, Lifetime: time.Duration(request.ExpiresInHours) * time.Hour, MaxUses: request.MaxUses,
	}, requestInfo(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, CreateEnrollmentTokenResponse{
		Token:             enrollmentToken(created.EnrollmentToken, h.clock.Now().UnixMilli()),
		Secret:            created.Token,
		ServerFingerprint: h.serverKey.Fingerprint(),
		TLSFingerprint:    h.tlsPin,
	})
}

func (h *handlers) handleRevokeEnrollmentToken(w http.ResponseWriter, r *http.Request, session auth.Session) {
	tokenID, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.fleet.RevokeEnrollmentToken(r.Context(), session, tokenID, requestInfo(r)); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func enrollmentToken(token fleet.EnrollmentToken, now int64) EnrollmentToken {
	return EnrollmentToken{
		ID: token.ID, Note: token.Note, CreatedBy: token.CreatedBy, CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt, MaxUses: token.MaxUses, UseCount: token.UseCount,
		Revoked: token.RevokedAt != nil, Usable: token.UsableAt(now),
	}
}

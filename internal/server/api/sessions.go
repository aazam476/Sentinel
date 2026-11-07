// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"strconv"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// SessionSummary is one of the user's signed-in sessions.
type SessionSummary struct {
	ID         int64  `json:"id"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	IP         string `json:"ip"`
	UserAgent  string `json:"userAgent"`
	Current    bool   `json:"current"`
}

// SessionsResponse lists the user's sessions (SEC-06).
type SessionsResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

func (h *handlers) handleListSessions(w http.ResponseWriter, r *http.Request, session auth.Session) {
	sessions, err := h.auth.ListSessions(r.Context(), session)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := SessionsResponse{Sessions: make([]SessionSummary, 0, len(sessions))}
	for _, summary := range sessions {
		response.Sessions = append(response.Sessions, SessionSummary(summary))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *handlers) handleRevokeSession(w http.ResponseWriter, r *http.Request, session auth.Session) {
	sessionID, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.auth.RevokeSession(r.Context(), session, sessionID, requestInfo(r)); err != nil {
		h.writeError(w, err)
		return
	}
	if sessionID == session.ID {
		h.clearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathID reads the {id} path value as a positive integer, writing a 404 and
// returning false when it isn't one.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, errorCodeNotFound, auth.ErrNotFound.Error())
		return 0, false
	}
	return id, true
}

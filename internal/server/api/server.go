// SPDX-License-Identifier: Apache-2.0

package api

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// sessionCookieName is the cookie that carries the session token.
const sessionCookieName = "sentinel_session"

// handlers holds what the HTTP handlers share.
type handlers struct {
	auth       *auth.Service
	auditLog   *audit.Log
	logger     *slog.Logger
	clock      Clock
	cookiePath string
	publicURL  *url.URL
}

// Clock tells the current time.
type Clock interface {
	Now() time.Time
}

// setSessionCookie sends the session token (SEC-06): HttpOnly so scripts
// can't read it, Secure so it never travels unencrypted, and SameSite=Strict
// so other sites can't make the browser send it (SEC-12). It lasts as long as
// a session can, so closing the browser doesn't sign the user out.
func (h *handlers) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     h.cookiePath,
		MaxAge:   int(auth.AbsoluteTimeout / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *handlers) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     h.cookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// sessionHandler handles a request for a valid session.
type sessionHandler func(w http.ResponseWriter, r *http.Request, session auth.Session)

// anySession runs next for any valid session, including one that has only
// passed the password step.
func (h *handlers) anySession(next sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		token := ""
		if err == nil {
			token = cookie.Value
		}
		session, err := h.auth.Authenticate(r.Context(), token)
		if err != nil {
			h.writeError(w, err)
			return
		}
		next(w, r, session)
	}
}

// signedIn runs next only for a session that has passed both login steps
// (SEC-04).
func (h *handlers) signedIn(next sessionHandler) http.HandlerFunc {
	return h.anySession(func(w http.ResponseWriter, r *http.Request, session auth.Session) {
		if !session.IsSignedIn() {
			h.writeError(w, auth.ErrNotAuthenticated)
			return
		}
		next(w, r, session)
	})
}

// withPermission runs next only for a signed-in session whose role has
// permission. Who has which permission is decided in auth/permissions.go.
func (h *handlers) withPermission(permission auth.Permission, next sessionHandler) http.HandlerFunc {
	return h.signedIn(func(w http.ResponseWriter, r *http.Request, session auth.Session) {
		if !session.Role.Can(permission) {
			h.writeError(w, auth.ErrForbidden)
			return
		}
		next(w, r, session)
	})
}

func (h *handlers) writeError(w http.ResponseWriter, err error) {
	writeServiceError(w, h.logger, err)
}

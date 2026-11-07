// SPDX-License-Identifier: Apache-2.0

package api

import (
	"log/slog"
	"net/http"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// RouterOptions holds what NewRouter needs to build the routes.
type RouterOptions struct {
	// WebUI serves the browser UI for every path that isn't an API route.
	WebUI    http.Handler
	Auth     *auth.Service
	AuditLog *audit.Log
	Logger   *slog.Logger
	Clock    Clock
	// BasePath is the configured base path; the session cookie is limited
	// to it.
	BasePath string
}

// NewRouter returns the handler for every URL the server answers.
func NewRouter(options RouterOptions) http.Handler {
	h := &handlers{
		auth:       options.Auth,
		auditLog:   options.AuditLog,
		logger:     options.Logger,
		clock:      options.Clock,
		cookiePath: cookiePath(options.BasePath),
	}

	mux := http.NewServeMux()
	// Without a method in the pattern, so a POST gets 405 here instead of
	// falling through to the web UI's "/" route.
	mux.HandleFunc("/healthz", handleHealthz)

	mux.HandleFunc("GET /api/setup/status", h.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/begin", h.handleBeginSetup)
	mux.HandleFunc("POST /api/setup/complete", h.handleCompleteSetup)

	mux.HandleFunc("POST /api/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/auth/login/second-factor", h.anySession(h.handleSecondFactor))
	mux.HandleFunc("POST /api/auth/totp/enrollment", h.anySession(h.handleBeginEnrollment))
	mux.HandleFunc("POST /api/auth/totp/enrollment/confirm", h.anySession(h.handleConfirmEnrollment))
	mux.HandleFunc("GET /api/auth/session", h.anySession(h.handleSession))
	mux.HandleFunc("POST /api/auth/logout", h.anySession(h.handleLogout))
	mux.HandleFunc("POST /api/auth/step-up", h.signedIn(h.handleStepUp))
	mux.HandleFunc("GET /api/auth/sessions", h.signedIn(h.handleListSessions))
	mux.HandleFunc("DELETE /api/auth/sessions/{id}", h.signedIn(h.handleRevokeSession))

	mux.HandleFunc("GET /api/users", h.withPermission(auth.PermissionManageUsers, h.handleListUsers))
	mux.HandleFunc("POST /api/users", h.withPermission(auth.PermissionManageUsers, h.handleCreateUser))
	mux.HandleFunc("PATCH /api/users/{id}", h.withPermission(auth.PermissionManageUsers, h.handleUpdateUser))

	mux.HandleFunc("GET /api/audit", h.withPermission(auth.PermissionViewAuditLog, h.handleListAudit))
	mux.HandleFunc("GET /api/audit/verify", h.withPermission(auth.PermissionViewAuditLog, h.handleVerifyAudit))
	mux.HandleFunc("GET /api/audit/export", h.withPermission(auth.PermissionViewAuditLog, h.handleExportAudit))

	mux.HandleFunc("/api/", handleUnknownAPIRoute)
	mux.Handle("/", options.WebUI)
	return mux
}

// cookiePath limits the session cookie to Sentinel's base path, so other
// applications on the same host never receive it.
func cookiePath(basePath string) string {
	if basePath == "" || basePath == "/" {
		return "/"
	}
	return basePath + "/"
}

// handleUnknownAPIRoute answers API paths that don't exist with a JSON 404,
// rather than letting them fall through to the web UI's HTML page.
func handleUnknownAPIRoute(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, errorCodeNotFound, "There is no API endpoint at this address.")
}

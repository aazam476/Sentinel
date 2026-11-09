// SPDX-License-Identifier: Apache-2.0

package api

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
	"github.com/aazam476/sentinel/internal/server/registry"
	"github.com/aazam476/sentinel/internal/server/relay"
	"github.com/aazam476/sentinel/internal/server/scripts"
	"github.com/aazam476/sentinel/internal/server/serverkey"
	"github.com/aazam476/sentinel/internal/server/updates"
)

// RouterOptions holds what NewRouter needs to build the routes.
type RouterOptions struct {
	// WebUI serves the browser UI for every path that isn't an API route.
	WebUI    http.Handler
	Auth     *auth.Service
	AuditLog *audit.Log
	Fleet    *fleet.Service
	// Hub tells which agents are online; the agents' own endpoint,
	// /agent/connect, is routed by internal/server/app.
	Hub       *hub.Hub
	Metrics   *metrics.Service
	Registry  *registry.Service
	Alerts    *alerts.Service
	Scripts   *scripts.Service
	Updates   *updates.Service
	ServerKey serverkey.Key
	// TLSFingerprint is the self-signed certificate's key fingerprint, or
	// "" when agents check the server's certificate the usual way.
	TLSFingerprint string
	Logger         *slog.Logger
	Clock          Clock
	// BasePath is the configured base path; the session cookie is limited
	// to it.
	BasePath string
	// PublicURL is the configured public origin, or nil. Passkeys belong to
	// its host name.
	PublicURL *url.URL
	// MaxFileBytes is the largest file the file manager moves; 0 is the
	// default, 10 GB.
	MaxFileBytes int64
}

// NewRouter returns the handler for every URL the server answers.
func NewRouter(options RouterOptions) http.Handler {
	h := &handlers{
		auth:         options.Auth,
		auditLog:     options.AuditLog,
		fleet:        options.Fleet,
		hub:          options.Hub,
		metrics:      options.Metrics,
		registries:   options.Registry,
		alerts:       options.Alerts,
		scripts:      options.Scripts,
		updates:      options.Updates,
		terminals:    relay.NewSessions(options.Clock),
		serverKey:    options.ServerKey,
		tlsPin:       options.TLSFingerprint,
		logger:       options.Logger,
		clock:        options.Clock,
		cookiePath:   cookiePath(options.BasePath),
		publicURL:    options.PublicURL,
		maxFileBytes: maxFileBytes(options.MaxFileBytes),
	}

	mux := http.NewServeMux()
	// Without a method in the pattern, so a POST gets 405 here instead of
	// falling through to the web UI's "/" route.
	mux.HandleFunc("/healthz", handleHealthz)
	h.addSignInRoutes(mux)
	h.addAccountRoutes(mux)
	h.addAdminRoutes(mux)
	h.addMachineRoutes(mux)
	h.addDockerRoutes(mux)
	h.addStorageRoutes(mux)
	h.addFileRoutes(mux)
	h.addScriptRoutes(mux)
	h.addAlertRoutes(mux)
	h.addUpdateRoutes(mux)
	mux.HandleFunc("/api/", handleUnknownAPIRoute)
	mux.Handle("/", options.WebUI)
	return mux
}

// addSignInRoutes adds first-boot setup and the sign-in steps.
func (h *handlers) addSignInRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/status", h.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/begin", h.handleBeginSetup)
	mux.HandleFunc("POST /api/setup/complete", h.handleCompleteSetup)

	mux.HandleFunc("POST /api/auth/login", h.handleLogin)
	mux.HandleFunc("POST /api/auth/login/second-factor", h.anySession(h.handleSecondFactor))
	mux.HandleFunc("POST /api/auth/totp/enrollment", h.anySession(h.handleBeginEnrollment))
	mux.HandleFunc("POST /api/auth/totp/enrollment/confirm", h.anySession(h.handleConfirmEnrollment))
	mux.HandleFunc("GET /api/auth/session", h.anySession(h.handleSession))
	mux.HandleFunc("POST /api/auth/logout", h.anySession(h.handleLogout))
	mux.HandleFunc("POST /api/auth/passkeys/assertion/begin", h.anySession(h.handleBeginPasskeyAssertion))
	mux.HandleFunc("POST /api/auth/passkeys/assertion/finish", h.anySession(h.handleFinishPasskeyAssertion))
}

// addAccountRoutes adds what a user manages about their own account.
func (h *handlers) addAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/step-up", h.signedIn(h.handleStepUp))
	mux.HandleFunc("GET /api/auth/sessions", h.signedIn(h.handleListSessions))
	mux.HandleFunc("DELETE /api/auth/sessions/{id}", h.signedIn(h.handleRevokeSession))
	mux.HandleFunc("POST /api/auth/passkeys/registration/begin", h.anySession(h.handleBeginPasskeyRegistration))
	mux.HandleFunc("POST /api/auth/passkeys/registration/finish", h.anySession(h.handleFinishPasskeyRegistration))
	mux.HandleFunc("GET /api/auth/passkeys", h.signedIn(h.handleListPasskeys))
	mux.HandleFunc("DELETE /api/auth/passkeys/{id}", h.signedIn(h.handleDeletePasskey))
}

// addAdminRoutes adds users, the audit log and enrollment tokens.
func (h *handlers) addAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/users", h.withPermission(auth.PermissionManageUsers, h.handleListUsers))
	mux.HandleFunc("POST /api/users", h.withPermission(auth.PermissionManageUsers, h.handleCreateUser))
	mux.HandleFunc("PATCH /api/users/{id}", h.withPermission(auth.PermissionManageUsers, h.handleUpdateUser))

	mux.HandleFunc("GET /api/audit", h.withPermission(auth.PermissionViewAuditLog, h.handleListAudit))
	mux.HandleFunc("GET /api/audit/verify", h.withPermission(auth.PermissionViewAuditLog, h.handleVerifyAudit))
	mux.HandleFunc("GET /api/audit/export", h.withPermission(auth.PermissionViewAuditLog, h.handleExportAudit))

	mux.HandleFunc("GET /api/enrollment-tokens", h.withPermission(auth.PermissionManageEnrollment, h.handleListEnrollmentTokens))
	mux.HandleFunc("POST /api/enrollment-tokens", h.withPermission(auth.PermissionManageEnrollment, h.handleCreateEnrollmentToken))
	mux.HandleFunc("DELETE /api/enrollment-tokens/{id}", h.withPermission(auth.PermissionManageEnrollment, h.handleRevokeEnrollmentToken))
}

// addMachineRoutes adds the fleet and everything done on one machine.
func (h *handlers) addMachineRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents", h.withPermission(auth.PermissionViewFleet, h.handleListAgents))
	mux.HandleFunc("GET /api/agents/{id}", h.withPermission(auth.PermissionViewFleet, h.handleGetAgent))
	mux.HandleFunc("PATCH /api/agents/{id}", h.withPermission(auth.PermissionManageMachines, h.handleUpdateAgent))
	mux.HandleFunc("POST /api/agents/{id}/revoke", h.withPermission(auth.PermissionRevokeAgents, h.handleRevokeAgent))
	mux.HandleFunc("GET /api/agents/{id}/metrics", h.withPermission(auth.PermissionViewFleet, h.handleAgentMetrics))
	mux.HandleFunc("GET /api/agents/{id}/terminal", h.withPermission(auth.PermissionUseTerminal, h.handleTerminal))
	mux.HandleFunc("GET /api/agents/{id}/services", h.withPermission(auth.PermissionViewFleet, h.handleListServices))
	mux.HandleFunc("POST /api/agents/{id}/services/{name}/{action}", h.withPermission(auth.PermissionManageServices, h.handleServiceAction))
	mux.HandleFunc("GET /api/agents/{id}/logs", h.withPermission(auth.PermissionViewFleet, h.handleQueryLogs))
	mux.HandleFunc("GET /api/agents/{id}/processes", h.withPermission(auth.PermissionManageProcesses, h.handleListProcesses))
	mux.HandleFunc("POST /api/agents/{id}/processes/{pid}/kill", h.withPermission(auth.PermissionManageProcesses, h.handleKillProcess))
	mux.HandleFunc("POST /api/agents/{id}/power", h.withPermission(auth.PermissionControlPower, h.handlePower))
	mux.HandleFunc("GET /api/events", h.withPermission(auth.PermissionViewFleet, h.handleEvents))
}

func (h *handlers) addDockerRoutes(mux *http.ServeMux) {
	view := func(handler func(http.ResponseWriter, *http.Request, auth.Session)) http.HandlerFunc {
		return h.withPermission(auth.PermissionViewFleet, handler)
	}
	manage := func(handler func(http.ResponseWriter, *http.Request, auth.Session)) http.HandlerFunc {
		return h.withPermission(auth.PermissionManageDocker, handler)
	}
	const docker = "/api/agents/{id}/docker"
	mux.HandleFunc("GET "+docker+"/containers", view(dockerList[protocol.ContainerListPayload](h, protocol.ActionDockerContainerList)))
	mux.HandleFunc("POST "+docker+"/containers/{container}/{action}", manage(h.handleContainerAction))
	mux.HandleFunc("DELETE "+docker+"/containers/{container}", manage(h.handleRemoveContainer))
	mux.HandleFunc("GET "+docker+"/containers/{container}/inspect", manage(h.handleInspectContainer))
	mux.HandleFunc("GET "+docker+"/containers/{container}/logs", view(h.handleContainerLogs))
	mux.HandleFunc("GET "+docker+"/containers/{container}/logs/follow", view(h.handleFollowLogs))
	mux.HandleFunc("GET "+docker+"/containers/{container}/exec", manage(h.handleContainerExec))
	mux.HandleFunc("GET "+docker+"/images", view(h.handleListImages))
	mux.HandleFunc("POST "+docker+"/images/pull", manage(h.handlePullImage))
	mux.HandleFunc("POST "+docker+"/images/check", manage(h.handleCheckImages))
	mux.HandleFunc("DELETE "+docker+"/images", manage(h.handleRemoveImage))
	mux.HandleFunc("GET "+docker+"/volumes", view(dockerList[protocol.VolumeListPayload](h, protocol.ActionDockerVolumeList)))
	mux.HandleFunc("DELETE "+docker+"/volumes/{name}", manage(h.handleRemoveVolume))
	mux.HandleFunc("GET "+docker+"/networks", view(dockerList[protocol.NetworkListPayload](h, protocol.ActionDockerNetworkList)))
	mux.HandleFunc("DELETE "+docker+"/networks/{name}", manage(h.handleRemoveNetwork))
	mux.HandleFunc("GET "+docker+"/stacks", view(dockerList[protocol.StackListPayload](h, protocol.ActionDockerStackList)))
	mux.HandleFunc("GET "+docker+"/stacks/{name}", manage(h.handleGetStack))
	mux.HandleFunc("PUT "+docker+"/stacks/{name}", manage(h.handleSaveStack))
	mux.HandleFunc("POST "+docker+"/stacks/{name}/{action}", manage(h.handleStackAction))
	mux.HandleFunc("GET /api/registries", h.withPermission(auth.PermissionManageSettings, h.handleListRegistryLogins))
	mux.HandleFunc("POST /api/registries", h.withPermission(auth.PermissionManageSettings, h.handleAddRegistryLogin))
	mux.HandleFunc("DELETE /api/registries/{loginId}", h.withPermission(auth.PermissionManageSettings, h.handleRemoveRegistryLogin))
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

func maxFileBytes(configured int64) int64 {
	if configured <= 0 {
		return 10 << 30
	}
	return configured
}

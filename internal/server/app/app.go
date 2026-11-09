// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
	"github.com/aazam476/sentinel/internal/server/middleware"
	"github.com/aazam476/sentinel/internal/server/registry"
	"github.com/aazam476/sentinel/internal/server/scheduler"
	"github.com/aazam476/sentinel/internal/server/scripts"
	"github.com/aazam476/sentinel/internal/server/secrets"
	"github.com/aazam476/sentinel/internal/server/serverkey"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/server/web"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// Clock tells the current time and makes timers.
type Clock interface {
	Now() time.Time
	NewTimer(duration time.Duration) clock.Timer
}

// Options holds what New needs.
type Options struct {
	Settings config.Config
	Logger   *slog.Logger
	Clock    Clock
	// WebUI overrides the embedded web UI; tests pass a stub. Nil means the
	// UI built into the binary.
	WebUI http.Handler
	// TLSFingerprint is the self-signed certificate's key fingerprint for
	// install commands, or "" when agents check the certificate normally.
	TLSFingerprint string
}

// App is an assembled server, ready to serve.
type App struct {
	// Handler answers every HTTP request.
	Handler   http.Handler
	Auth      *auth.Service
	AuditLog  *audit.Log
	Fleet     *fleet.Service
	Hub       *hub.Hub
	Metrics   *metrics.Service
	Registry  *registry.Service
	Alerts    *alerts.Service
	Scripts   *scripts.Service
	ServerKey serverkey.Key
	database  *store.Store
	clock     Clock
	logger    *slog.Logger
	// background runs the scheduler started by Start.
	background sync.WaitGroup
}

// services are the parts New assembles before building the handler.
type services struct {
	auth      *auth.Service
	auditLog  *audit.Log
	fleet     *fleet.Service
	hub       *hub.Hub
	metrics   *metrics.Service
	registry  *registry.Service
	alerts    *alerts.Service
	scripts   *scripts.Service
	serverKey serverkey.Key
}

// New creates the data directory if needed, opens the database and secrets
// key, and builds the handler. Call Close when done.
func New(ctx context.Context, options Options) (*App, error) {
	settings := options.Settings
	if err := os.MkdirAll(settings.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	database, err := store.Open(ctx, filepath.Join(settings.DataDir, "sentinel.db"), options.Clock)
	if err != nil {
		return nil, err
	}
	built, err := buildServices(database, options)
	if err != nil {
		return nil, errors.Join(err, database.Close())
	}
	handler, err := buildHandler(options, built)
	if err != nil {
		return nil, errors.Join(err, database.Close())
	}
	return &App{
		Handler: handler, Auth: built.auth, AuditLog: built.auditLog, Fleet: built.fleet, Hub: built.hub,
		Metrics: built.metrics, Registry: built.registry, Alerts: built.alerts, Scripts: built.scripts, ServerKey: built.serverKey, database: database, clock: options.Clock, logger: options.Logger,
	}, nil
}

func buildServices(database *store.Store, options Options) (services, error) {
	dataDir := options.Settings.DataDir
	box, err := secrets.LoadOrCreateKey(dataDir)
	if err != nil {
		return services{}, err
	}
	serverKey, err := serverkey.LoadOrCreate(dataDir)
	if err != nil {
		return services{}, err
	}
	auditLog := audit.New(database)
	built := services{
		auth: auth.NewService(auth.Options{
			Database: database, Secrets: box, AuditLog: auditLog, Clock: options.Clock, DataDir: dataDir,
		}),
		auditLog:  auditLog,
		fleet:     fleet.NewService(fleet.Options{Database: database, AuditLog: auditLog, Clock: options.Clock}),
		metrics:   metrics.NewService(database, options.Clock),
		registry:  registry.NewService(database, auditLog, box, options.Clock),
		serverKey: serverKey,
	}
	// The hub, built below, tells alerts which machines are online.
	built.alerts = alerts.NewService(alerts.Options{
		Database: database, AuditLog: auditLog, Sealer: box, Clock: options.Clock, Logger: options.Logger,
		Online:  func(agentID int64) bool { return built.hub.IsOnline(agentID) },
		Changed: func(agentID int64, publicID string) { built.hub.AlertsChanged(agentID, publicID) },
	})
	built.hub = hub.New(hub.Options{
		Fleet: built.fleet, Metrics: built.metrics, Alerts: built.alerts, Key: serverKey, Clock: options.Clock, Logger: options.Logger,
	})
	built.scripts = scripts.New(scripts.Options{
		Database: database, AuditLog: auditLog, Hub: scripts.HubOf(built.hub), Fleet: built.fleet, Clock: options.Clock, Logger: options.Logger,
		Alerts: built.alerts,
	})
	return built, nil
}

// Start runs the background work (metric rollups every minute, cleanup
// every hour) until ctx ends. When it does, it closes every agent
// connection and browser event stream, so the HTTP server's graceful
// shutdown isn't left waiting on them.
func (a *App) Start(ctx context.Context) {
	a.background.Go(func() {
		scheduler.Run(ctx, a.clock, a.logger,
			scheduler.Job{Name: "metric rollup", Interval: time.Minute, Run: a.Metrics.Rollup},
			scheduler.Job{Name: "metric cleanup", Interval: time.Hour, Run: a.Metrics.Prune},
			scheduler.Job{Name: "image update check", Interval: 24 * time.Hour, Run: a.checkImages},
			scheduler.Job{Name: "scheduled scripts", Interval: time.Minute, Run: a.Scripts.RunDue},
			scheduler.Job{Name: "alert rules", Interval: time.Minute, Run: a.Alerts.Evaluate},
		)
		a.Hub.Shutdown()
		// Closing the agents' connections ends the scripts running on them.
		a.Scripts.Wait()
		// Notifications already on their way are let finish.
		a.Alerts.Wait()
	})
}

// checkImages checks every online machine's images for updates (§9.4).
// A machine that can't be checked (no Docker, offline by now) is skipped.
func (a *App) checkImages(ctx context.Context) error {
	for _, agentID := range a.Hub.Online() {
		if _, err := a.Registry.CheckMachine(ctx, a.Hub, agentID); err != nil {
			a.logger.Debug("skipped a machine's image update check", "agent", agentID, "error", err)
		}
	}
	return nil
}

// Close waits for the work Start began, closes any agent connections
// still open, and closes the database.
func (a *App) Close() error {
	a.background.Wait()
	a.Hub.Shutdown()
	return a.database.Close()
}

// AgentPath is where agents connect, under the base path.
const AgentPath = "/agent/connect"

// withAgentEndpoint sends AgentPath to the hub and everything else to
// browserRoutes. The agents' endpoint skips the browser Origin check:
// agents send no Origin header, and the hub refuses any request that has
// one, which is stricter still (SEC-13).
func withAgentEndpoint(browserRoutes http.Handler, agents *hub.Hub) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(AgentPath, agents)
	mux.Handle("/", browserRoutes)
	return mux
}

// buildHandler assembles the routes and wraps them in the protections every
// request needs. The order is described in internal/server/middleware.
func buildHandler(options Options, built services) (http.Handler, error) {
	settings := options.Settings
	webUI := options.WebUI
	if webUI == nil {
		embedded, err := web.Handler(settings.BasePath)
		if err != nil {
			return nil, err
		}
		webUI = embedded
	}

	handler := api.NewRouter(api.RouterOptions{
		WebUI:          webUI,
		Auth:           built.auth,
		AuditLog:       built.auditLog,
		Fleet:          built.fleet,
		Hub:            built.hub,
		Metrics:        built.metrics,
		Registry:       built.registry,
		Alerts:         built.alerts,
		Scripts:        built.scripts,
		ServerKey:      built.serverKey,
		TLSFingerprint: options.TLSFingerprint,
		Logger:         options.Logger,
		Clock:          options.Clock,
		BasePath:       settings.BasePath,
		PublicURL:      settings.PublicURL,
		MaxFileBytes:   settings.MaxFileBytes,
	})
	handler = middleware.RequireSameOrigin(handler, settings.PublicURL)
	handler = withAgentEndpoint(handler, built.hub)
	handler = middleware.WithClientInfo(handler, settings.TrustedProxies)
	handler = middleware.WithBasePath(handler, settings.BasePath)
	handler = middleware.SecurityHeaders(handler, middleware.SecurityHeadersOptions{
		// Only a certificate from a real authority is safe to pin with HSTS.
		StrictTransportSecurity: settings.TLS.Mode == config.TLSACME,
	})
	return handler, nil
}

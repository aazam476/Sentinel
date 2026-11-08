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

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
	"github.com/aazam476/sentinel/internal/server/middleware"
	"github.com/aazam476/sentinel/internal/server/scheduler"
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
		Metrics: built.metrics, ServerKey: built.serverKey, database: database, clock: options.Clock, logger: options.Logger,
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
		serverKey: serverKey,
	}
	built.hub = hub.New(hub.Options{
		Fleet: built.fleet, Metrics: built.metrics, Key: serverKey, Clock: options.Clock, Logger: options.Logger,
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
		)
		a.Hub.Shutdown()
	})
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
		WebUI:     webUI,
		Auth:      built.auth,
		AuditLog:  built.auditLog,
		Fleet:     built.fleet,
		Hub:       built.hub,
		Metrics:   built.metrics,
		ServerKey: built.serverKey,
		Logger:    options.Logger,
		Clock:     options.Clock,
		BasePath:  settings.BasePath,
		PublicURL: settings.PublicURL,
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

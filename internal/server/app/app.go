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
	"time"

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/middleware"
	"github.com/aazam476/sentinel/internal/server/secrets"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/server/web"
)

// Clock tells the current time.
type Clock interface {
	Now() time.Time
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
	Handler  http.Handler
	Auth     *auth.Service
	AuditLog *audit.Log
	database *store.Store
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
	box, err := secrets.LoadOrCreateKey(settings.DataDir)
	if err != nil {
		return nil, errors.Join(err, database.Close())
	}

	auditLog := audit.New(database)
	authService := auth.NewService(auth.Options{
		Database: database, Secrets: box, AuditLog: auditLog, Clock: options.Clock, DataDir: settings.DataDir,
	})
	handler, err := buildHandler(options, authService, auditLog)
	if err != nil {
		return nil, errors.Join(err, database.Close())
	}
	return &App{Handler: handler, Auth: authService, AuditLog: auditLog, database: database}, nil
}

// Close closes the database.
func (a *App) Close() error {
	return a.database.Close()
}

// buildHandler assembles the routes and wraps them in the protections every
// request needs. The order is described in internal/server/middleware.
func buildHandler(options Options, authService *auth.Service, auditLog *audit.Log) (http.Handler, error) {
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
		WebUI:    webUI,
		Auth:     authService,
		AuditLog: auditLog,
		Logger:   options.Logger,
		Clock:    options.Clock,
		BasePath: settings.BasePath,
	})
	handler = middleware.RequireSameOrigin(handler, settings.PublicURL)
	handler = middleware.WithClientInfo(handler, settings.TrustedProxies)
	handler = middleware.WithBasePath(handler, settings.BasePath)
	handler = middleware.SecurityHeaders(handler, middleware.SecurityHeadersOptions{
		// Only a certificate from a real authority is safe to pin with HSTS.
		StrictTransportSecurity: settings.TLS.Mode == config.TLSACME,
	})
	return handler, nil
}

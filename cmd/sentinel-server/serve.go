// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/httpserver"
	"github.com/aazam476/sentinel/internal/server/middleware"
	"github.com/aazam476/sentinel/internal/server/servertls"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/server/web"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// serve runs the server until the process receives an interrupt or
// terminate signal.
func serve(logger *slog.Logger, settings config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(settings.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	database, err := store.Open(ctx, filepath.Join(settings.DataDir, "sentinel.db"), clock.System{})
	if err != nil {
		return err
	}
	defer closeDatabase(logger, database)

	handler, err := buildHandler(settings)
	if err != nil {
		return err
	}
	tlsConfig, err := buildTLSConfig(settings)
	if err != nil {
		return err
	}

	listener, err := httpserver.Listen(ctx, settings.ListenAddress)
	if err != nil {
		return err
	}
	scheme := "http"
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
		scheme = "https"
	}

	logger.Info("server listening",
		"address", listener.Addr().String(),
		"url", scheme+"://"+listener.Addr().String()+settings.BasePath,
		"tls", string(settings.TLS.Mode),
		"version", buildinfo.Version())
	if err := httpserver.Serve(ctx, listener, handler, logger); err != nil {
		return err
	}
	logger.Info("server stopped")
	return nil
}

// closeDatabase closes the database at shutdown. A failure is logged rather
// than returned because the server is stopping either way.
func closeDatabase(logger *slog.Logger, database *store.Store) {
	if err := database.Close(); err != nil {
		logger.Error("close database", "error", err)
	}
}

// buildHandler assembles the routes and wraps them in the protections every
// request needs. The order is described in internal/server/middleware.
func buildHandler(settings config.Config) (http.Handler, error) {
	webUI, err := web.Handler(settings.BasePath)
	if err != nil {
		return nil, err
	}

	handler := api.NewRouter(api.RouterOptions{WebUI: webUI})
	handler = middleware.RequireSameOrigin(handler, settings.PublicURL)
	handler = middleware.WithClientInfo(handler, settings.TrustedProxies)
	handler = middleware.WithBasePath(handler, settings.BasePath)
	handler = middleware.SecurityHeaders(handler, middleware.SecurityHeadersOptions{
		// Only a certificate from a real authority is safe to pin with HSTS.
		StrictTransportSecurity: settings.TLS.Mode == config.TLSACME,
	})
	return handler, nil
}

// buildTLSConfig returns the TLS settings for the configured mode, or nil
// when the server serves plain HTTP.
func buildTLSConfig(settings config.Config) (*tls.Config, error) {
	switch settings.TLS.Mode {
	case config.TLSOff:
		return nil, nil
	case config.TLSACME:
		return servertls.ACME(servertls.ACMEOptions{
			DataDir:  settings.DataDir,
			HostName: settings.PublicURL.Hostname(),
			Email:    settings.TLS.ACMEEmail,
		}), nil
	default:
		return servertls.SelfSigned(servertls.SelfSignedOptions{
			DataDir: settings.DataDir,
			Hosts:   selfSignedHosts(settings),
			Clock:   clock.System{},
		})
	}
}

// selfSignedHosts lists every name the server is likely to be reached by, so
// the certificate matches whichever one the browser uses.
func selfSignedHosts(settings config.Config) []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		hosts = append(hosts, hostname)
	}
	if settings.PublicURL != nil {
		hosts = append(hosts, settings.PublicURL.Hostname())
	}
	if listenHost, _, err := net.SplitHostPort(settings.ListenAddress); err == nil && isSpecificHost(listenHost) {
		hosts = append(hosts, listenHost)
	}
	return hosts
}

// isSpecificHost reports whether host names one address, as opposed to ""
// or "0.0.0.0" / "::", which mean every interface and can't be in a
// certificate.
func isSpecificHost(host string) bool {
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsUnspecified()
}

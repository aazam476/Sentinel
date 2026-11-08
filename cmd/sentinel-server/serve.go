// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/aazam476/sentinel/internal/server/app"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/httpserver"
	"github.com/aazam476/sentinel/internal/server/servertls"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// serve runs the server until the process receives an interrupt or
// terminate signal. stdout receives the setup token on a fresh install.
func serve(logger *slog.Logger, stdout io.Writer, settings config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tlsConfig, err := buildTLSConfig(settings)
	if err != nil {
		return err
	}
	server, err := app.New(ctx, app.Options{
		Settings: settings, Logger: logger, Clock: clock.System{}, TLSFingerprint: tlsFingerprint(settings, tlsConfig),
	})
	if err != nil {
		return err
	}
	defer closeApp(logger, server)

	if err := announceSetupToken(ctx, server.Auth, stdout, logger); err != nil {
		return err
	}
	server.Start(ctx)
	return listenAndServe(ctx, logger, settings, tlsConfig, server.Handler)
}

// tlsFingerprint is the self-signed certificate's key fingerprint, which
// install commands carry so agents can trust that certificate (§7.2). With
// ACME or a TLS proxy, agents check the certificate the usual way instead.
func tlsFingerprint(settings config.Config, tlsConfig *tls.Config) string {
	if settings.TLS.Mode != config.TLSSelfSigned || tlsConfig == nil || len(tlsConfig.Certificates) == 0 ||
		tlsConfig.Certificates[0].Leaf == nil {
		return ""
	}
	return servertls.PublicKeyFingerprint(tlsConfig.Certificates[0].Leaf)
}

// listenAndServe opens the listening socket, adds TLS when it is on, and
// serves until ctx is canceled.
func listenAndServe(ctx context.Context, logger *slog.Logger, settings config.Config, tlsConfig *tls.Config, handler http.Handler) error {
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

// announceSetupToken prints the setup token while no admin exists (SEC-01),
// so the person installing Sentinel can find it in the terminal or in
// `docker logs`. It is also in <data dir>/setup-token.
func announceSetupToken(ctx context.Context, authService *auth.Service, stdout io.Writer, logger *slog.Logger) error {
	token, err := authService.PrepareSetupToken(ctx)
	if err != nil {
		return err
	}
	if token == "" {
		return nil
	}
	logger.Info("no admin exists yet; open the web UI and enter the setup token printed below")
	_, _ = fmt.Fprintf(stdout, "\nSentinel setup token: %s\n\n", token) // nothing more to do if stdout is gone
	return nil
}

// closeApp closes the database at shutdown. A failure is logged rather
// than returned because the server is stopping either way.
func closeApp(logger *slog.Logger, server *app.App) {
	if err := server.Close(); err != nil {
		logger.Error("close database", "error", err)
	}
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

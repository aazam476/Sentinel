// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// readHeaderTimeout limits how long a client may take to send request
// headers. Without it, a client that opens connections and never finishes
// its headers can tie up the server indefinitely (a "slowloris" attack).
const readHeaderTimeout = 10 * time.Second

// shutdownTimeout is how long in-flight requests get to finish after the
// server is asked to stop, before their connections are closed anyway.
const shutdownTimeout = 10 * time.Second

// Serve serves HTTP requests on listener with handler until ctx is canceled,
// then shuts down gracefully. It takes ownership of listener and closes it.
//
// Connection-level problems the HTTP server can't attribute to a request,
// such as failed TLS handshakes, go to logger at debug level: with a
// self-signed certificate every browser that hasn't accepted it yet causes
// one, so they are expected noise rather than warnings.
//
// It returns nil after a graceful shutdown, or the error that stopped the
// server otherwise.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler, logger *slog.Logger) error {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelDebug),
	}

	shutdownDone := make(chan error, 1)

	// This goroutine lives until either ctx is canceled (it then shuts the
	// server down) or Serve returns for another reason (stopWaiting closes).
	stopWaiting := make(chan struct{})
	defer close(stopWaiting)
	go func() {
		select {
		case <-ctx.Done():
			shutdownDone <- shutdown(server, shutdownTimeout)
		case <-stopWaiting:
		}
	}()

	err := server.Serve(listener)
	if !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http on %s: %w", listener.Addr(), err)
	}

	return <-shutdownDone
}

// shutdown stops server from accepting new connections and waits up to
// timeout for in-flight requests to finish.
func shutdown(server *http.Server, timeout time.Duration) error {
	// The caller's context is already canceled at this point, so the timeout
	// needs a fresh one.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down http server: %w", err)
	}
	return nil
}

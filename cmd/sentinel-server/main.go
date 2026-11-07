// SPDX-License-Identifier: Apache-2.0

// Command sentinel-server runs the central Sentinel server: the web UI today,
// and the API, database and agent connections as they are built.
//
// This file only wires things together. The logic lives in internal/server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aazam476/sentinel/internal/server/httpserver"
	"github.com/aazam476/sentinel/internal/server/web"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
)

// defaultListenAddress is loopback-only because TLS isn't implemented yet.
const defaultListenAddress = "127.0.0.1:7443"

// Exit codes. 2 matches what the flag package uses for usage errors.
const (
	exitOK         = 0
	exitFailure    = 1
	exitUsageError = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one invocation of the command and returns its exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "version" {
		_, _ = fmt.Fprintln(stdout, "sentinel-server", buildinfo.Version()) // nothing to do if stdout is gone
		return exitOK
	}

	flags := flag.NewFlagSet("sentinel-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(flags) }
	listenAddress := flags.String("listen", defaultListenAddress,
		"address to serve HTTP on; must be a loopback address until TLS is supported")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsageError
	}
	if flags.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "sentinel-server: unknown command %q\n\n", flags.Arg(0))
		printUsage(flags)
		return exitUsageError
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	if err := serve(logger, *listenAddress); err != nil {
		logger.Error("server failed", "error", err)
		return exitFailure
	}
	return exitOK
}

// serve runs the HTTP server until the process receives an interrupt or
// terminate signal.
func serve(logger *slog.Logger, listenAddress string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler, err := web.Handler()
	if err != nil {
		return err
	}

	listener, err := httpserver.Listen(ctx, listenAddress)
	if err != nil {
		return err
	}

	logger.Info("server listening", "address", listener.Addr().String(), "version", buildinfo.Version())
	if err := httpserver.Serve(ctx, listener, handler); err != nil {
		return err
	}
	logger.Info("server stopped")
	return nil
}

func printUsage(flags *flag.FlagSet) {
	output := flags.Output()
	_, _ = fmt.Fprintln(output, "Usage:")
	_, _ = fmt.Fprintln(output, "  sentinel-server [flags]    run the server")
	_, _ = fmt.Fprintln(output, "  sentinel-server version    print the version and exit")
	_, _ = fmt.Fprintln(output, "\nFlags:")
	flags.PrintDefaults()
}

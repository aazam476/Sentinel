// SPDX-License-Identifier: Apache-2.0

// Command sentinel-server runs the central Sentinel server: the web UI, the
// API, the database and, later, the agent connections.
//
// This file only parses the command line. serve.go wires the packages in
// internal/server together.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
)

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
	if len(args) > 0 {
		switch args[0] {
		case "version":
			_, _ = fmt.Fprintln(stdout, "sentinel-server", buildinfo.Version()) // nothing to do if stdout is gone
			return exitOK
		case "audit":
			return runAudit(args[1:], stdout, stderr)
		}
	}

	flags := flag.NewFlagSet("sentinel-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(flags) }
	configPath := flags.String("config", config.DefaultPath, "path to server.toml")
	listen := flags.String("listen", "", "address to listen on, overriding the config file (default \""+config.DefaultListen+"\")")
	dataDir := flags.String("data-dir", "", "data directory, overriding the config file (default \""+config.DefaultDataDir+"\")")
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
	settings, err := loadConfig(*configPath, *listen, *dataDir)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return exitFailure
	}
	if err := serve(logger, stdout, settings); err != nil {
		logger.Error("server failed", "error", err)
		return exitFailure
	}
	return exitOK
}

// loadConfig reads the config file and applies command-line overrides.
func loadConfig(path, listen, dataDir string) (config.Config, error) {
	file, err := config.Load(path)
	if err != nil {
		return config.Config{}, err
	}
	if listen != "" {
		file.Listen = listen
	}
	if dataDir != "" {
		file.DataDir = dataDir
	}
	return file.Resolve()
}

func printUsage(flags *flag.FlagSet) {
	output := flags.Output()
	_, _ = fmt.Fprintln(output, "Usage:")
	_, _ = fmt.Fprintln(output, "  sentinel-server [flags]    run the server")
	_, _ = fmt.Fprintln(output, "  sentinel-server version    print the version and exit")
	_, _ = fmt.Fprintln(output, "  sentinel-server audit verify [--data-dir dir]")
	_, _ = fmt.Fprintln(output, "                             check the audit log's hash chain")
	_, _ = fmt.Fprintln(output, "  sentinel-server audit export [--data-dir dir] [--format json|csv]")
	_, _ = fmt.Fprintln(output, "                             write the audit log to stdout")
	_, _ = fmt.Fprintln(output, "\nFlags:")
	flags.PrintDefaults()
}

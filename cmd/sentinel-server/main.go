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
	"github.com/aazam476/sentinel/internal/shared/release"
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
			_, _ = fmt.Fprintln(stdout, "sentinel-server", buildinfo.Version()+release.BuildNote()) // nothing to do if stdout is gone
			return exitOK
		case "audit":
			return runAudit(args[1:], stdout, stderr)
		case "reset-admin":
			return runResetAdmin(args[1:], stdout, stderr)
		case "backup":
			return runBackup(args[1:], os.Stdin, stdout, stderr)
		case "restore":
			return runRestore(args[1:], os.Stdin, stdout, stderr)
		}
	}

	flags := flag.NewFlagSet("sentinel-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { printUsage(flags) }
	configPath := flags.String("config", config.DefaultPath, "path to server.toml")
	listen := flags.String("listen", "", "address to listen on, overriding the config file (default \""+config.DefaultListen+"\")")
	dataDir := flags.String("data-dir", "", "data directory, overriding the config file (default \""+config.DefaultDataDir+"\")")
	relayIP := flags.String("relay-ip", "", "the address remote desktop's relay is reached at, overriding the config file (in Docker, the host's)")
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
	settings, err := loadConfig(*configPath, overrides{listen: *listen, dataDir: *dataDir, relayIP: *relayIP})
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		return exitFailure
	}
	if err := serve(logger, settings); err != nil {
		logger.Error("server failed", "error", err)
		return exitFailure
	}
	return exitOK
}

// overrides are the settings given on the command line.
type overrides struct {
	listen, dataDir, relayIP string
}

// loadConfig reads the config file and applies command-line overrides.
func loadConfig(path string, given overrides) (config.Config, error) {
	file, err := config.Load(path)
	if err != nil {
		return config.Config{}, err
	}
	if given.listen != "" {
		file.Listen = given.listen
	}
	if given.dataDir != "" {
		file.DataDir = given.dataDir
	}
	if given.relayIP != "" {
		file.TURN.RelayIP = given.relayIP
	}
	return file.Resolve()
}

func printUsage(flags *flag.FlagSet) {
	output := flags.Output()
	_, _ = fmt.Fprintln(output, "Usage:")
	_, _ = fmt.Fprintln(output, "  sentinel-server [flags]    run the server")
	_, _ = fmt.Fprintln(output, "  sentinel-server version    print the version and exit")
	_, _ = fmt.Fprintln(output, "  sentinel-server reset-admin [--data-dir dir] [--username name]")
	_, _ = fmt.Fprintln(output, "                             reset an admin's password and 2FA (run on the server host)")
	_, _ = fmt.Fprintln(output, "  sentinel-server backup [--data-dir dir] [--passphrase-file f] <file>")
	_, _ = fmt.Fprintln(output, "                             write an encrypted backup of the database, keys and config")
	_, _ = fmt.Fprintln(output, "  sentinel-server restore [--data-dir dir] [--passphrase-file f] [--force] <file>")
	_, _ = fmt.Fprintln(output, "                             restore a backup (stop the server first)")
	_, _ = fmt.Fprintln(output, "  sentinel-server audit verify [--data-dir dir]")
	_, _ = fmt.Fprintln(output, "                             check the audit log's hash chain")
	_, _ = fmt.Fprintln(output, "  sentinel-server audit export [--data-dir dir] [--format json|csv]")
	_, _ = fmt.Fprintln(output, "                             write the audit log to stdout")
	_, _ = fmt.Fprintln(output, "\nFlags:")
	flags.PrintDefaults()
}

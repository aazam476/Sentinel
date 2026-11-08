// SPDX-License-Identifier: Apache-2.0

// Command sentinel-agent is the program installed on every managed machine.
//
//	sentinel-agent install   --server URL --token T --fingerprint FP [--tls-fingerprint FP] [--dry-run]
//	sentinel-agent enroll    (the same flags; enrolls without installing the service)
//	sentinel-agent run       (what the service runs; also works in a terminal)
//	sentinel-agent uninstall [--dry-run]
//	sentinel-agent version
//
// This file only parses arguments and wires things together. The logic
// lives in internal/agent.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/aazam476/sentinel/internal/agent/identity"
	"github.com/aazam476/sentinel/internal/agent/setup"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
)

// Exit codes. 2 is the conventional code for a usage error.
const (
	exitOK         = 0
	exitFailure    = 1
	exitUsageError = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one invocation of the command and returns its exit code.
// Output that fails to write is ignored: there is nowhere else to report it.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitUsageError
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(stdout, "sentinel-agent", buildinfo.Version())
		return exitOK
	case "help", "-h", "-help", "--help":
		printUsage(stdout)
		return exitOK
	case "install", "enroll":
		return runEnroll(args[0], args[1:], stdout, stderr)
	case "uninstall":
		return runUninstall(args[1:], stdout, stderr)
	case "run":
		return runAgent(args[1:], stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "sentinel-agent: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return exitUsageError
	}
}

// pathFlags adds --state-dir and --config-dir, which move the agent's
// files away from the standard places (for tests, or unusual layouts).
func pathFlags(flags *flag.FlagSet) *identity.Paths {
	paths := identity.PathsForThisMachine()
	flags.StringVar(&paths.StateDir, "state-dir", paths.StateDir, "directory for the agent's key and identity")
	flags.StringVar(&paths.ConfigDir, "config-dir", paths.ConfigDir, "directory for policy.toml")
	return &paths
}

func runEnroll(command string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	paths := pathFlags(flags)
	var server setup.Server
	flags.StringVar(&server.URL, "server", "", "the server's URL, e.g. https://sentinel.example.com")
	flags.StringVar(&server.Token, "token", "", "the enrollment token from the server's Add machine page")
	flags.StringVar(&server.Fingerprint, "fingerprint", "", "the server key fingerprint from the install command")
	flags.StringVar(&server.TLSFingerprint, "tls-fingerprint", "", "the server's self-signed certificate fingerprint, if it uses one")
	dryRun := flags.Bool("dry-run", false, "describe the steps without changing anything")
	if err := flags.Parse(args); err != nil {
		return exitUsageError
	}
	if server.URL == "" || server.Token == "" || server.Fingerprint == "" {
		_, _ = fmt.Fprintf(stderr, "sentinel-agent %s: --server, --token and --fingerprint are required; copy the command from the server's Add machine page\n", command)
		return exitUsageError
	}

	options := setupOptions(*paths, stdout, *dryRun)
	ctx, stop := signalContext()
	defer stop()
	var err error
	if command == "install" {
		err = setup.Install(ctx, options, server)
	} else {
		err = setup.Enroll(ctx, options, server)
	}
	return report(command, err, stderr)
}

func runUninstall(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	paths := pathFlags(flags)
	dryRun := flags.Bool("dry-run", false, "describe the steps without changing anything")
	if err := flags.Parse(args); err != nil {
		return exitUsageError
	}
	return report("uninstall", setup.Uninstall(setupOptions(*paths, stdout, *dryRun)), stderr)
}

// report prints a failed command's error and turns it into an exit code.
func report(command string, err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	_, _ = fmt.Fprintf(stderr, "sentinel-agent %s: %v\n", command, err)
	if errors.Is(err, os.ErrPermission) {
		_, _ = fmt.Fprintln(stderr, "Run it as root (sudo), or as an administrator on Windows.")
	}
	return exitFailure
}

func printUsage(output io.Writer) {
	lines := []string{
		"Usage:",
		"  sentinel-agent install --server URL --token TOKEN --fingerprint FP [--tls-fingerprint FP] [--dry-run]",
		"                              enroll this machine and install the service",
		"  sentinel-agent enroll  ...  the same, without installing the service",
		"  sentinel-agent run          connect and serve (what the service runs)",
		"  sentinel-agent uninstall [--dry-run]",
		"                              remove the service and the agent's files",
		"  sentinel-agent version      print the version and exit",
		"  sentinel-agent help         show this help",
	}
	for _, line := range lines {
		_, _ = fmt.Fprintln(output, line)
	}
}

// SPDX-License-Identifier: Apache-2.0

// Command sentinel-agent is the program installed on every managed machine.
//
// Only the version subcommand exists so far. The install, enroll, run,
// uninstall and desktop-helper subcommands are added together with the
// features they belong to.
//
// This file only wires things together. The logic lives in internal/agent.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/aazam476/sentinel/internal/shared/buildinfo"
)

// Exit codes. 2 is the conventional code for a usage error.
const (
	exitOK         = 0
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
	default:
		_, _ = fmt.Fprintf(stderr, "sentinel-agent: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return exitUsageError
	}
}

func printUsage(output io.Writer) {
	_, _ = fmt.Fprintln(output, "Usage:")
	_, _ = fmt.Fprintln(output, "  sentinel-agent version    print the version and exit")
	_, _ = fmt.Fprintln(output, "  sentinel-agent help       show this help")
}

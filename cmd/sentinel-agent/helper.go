// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"

	"github.com/aazam476/sentinel/internal/agent/desktop/helper"
)

// runDesktopHelper serves this session's desktop to the agent over
// standard input and output (§9.3): the agent starts it, as
// `sentinel-agent desktop-helper`, in the session of whoever is at the
// screen.
func runDesktopHelper(stderr io.Writer) int {
	// Windows ties a thread, not a process, to a desktop: every call that
	// touches the screen must come from this one thread.
	runtime.LockOSThread()
	source, err := openHelperSource()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sentinel-agent desktop-helper:", err)
		return exitFailure
	}
	defer func() { _ = source.Close() }() // the session is over either way
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := helper.Serve(ctx, source, os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "sentinel-agent desktop-helper:", err)
		return exitFailure
	}
	return exitOK
}

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"

	"github.com/aazam476/sentinel/internal/agent/identity"
	"github.com/aazam476/sentinel/internal/agent/tray"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// pauseFileName records, in the state folder, that the person at the
// machine paused remote access.
const pauseFileName = "remote-access-paused"

// errTrayRunning means this user's tray is already running.
var errTrayRunning = errors.New("the Sentinel tray is already running for this user")

// trayUI is the native tray: winui on Windows, macui on macOS.
type trayUI interface {
	tray.UI
	Run(ctx context.Context) error
}

// traySocket opens the socket the trays connect to, on systems with a
// tray. A socket that can't be opened leaves the agent without one: it
// still runs, and asks before desktop sessions the way it otherwise would.
func traySocket(paths identity.Paths, logger *slog.Logger) *tray.Server {
	if !tray.Supported(runtime.GOOS) {
		return nil
	}
	listener, err := tray.Listen(tray.SocketPathForThisMachine())
	if err != nil {
		logger.Warn("the tray can't reach this agent", "error", err)
		return nil
	}
	return tray.NewServer(tray.ServerOptions{
		Listener: listener, PauseFile: filepath.Join(paths.StateDir, pauseFileName), Logger: logger,
	})
}

// runTray is `sentinel-agent tray`, started for each person who signs in.
func runTray(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("tray", flag.ContinueOnError)
	flags.SetOutput(stderr)
	socket := flags.String("socket", tray.SocketPathForThisMachine(), "the agent's tray socket")
	if err := flags.Parse(args); err != nil {
		return exitUsageError
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	actions := make(chan tray.Action, 8)
	ui, err := newTrayUI(func(action tray.Action) {
		select {
		case actions <- action:
		default: // clicks faster than the agent takes them are dropped
		}
	})
	if errors.Is(err, errTrayRunning) {
		return exitOK
	}
	if err != nil {
		return report("tray", err, stderr)
	}
	ctx, stop := signalContext()
	defer stop()
	dial := func(ctx context.Context) (net.Conn, error) { return tray.Dial(ctx, *socket) }
	go tray.RunClient(ctx, tray.ClientOptions{Dial: dial, UI: ui, Clock: clock.System{}, Logger: logger}, actions)
	return report("tray", ui.Run(ctx), stderr)
}

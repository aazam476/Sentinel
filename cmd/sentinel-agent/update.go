// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/identity"
	"github.com/aazam476/sentinel/internal/agent/update"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// exitRestart ends the process so the service manager starts it again:
// systemd and launchd restart the agent whenever it exits, and Windows
// does on any exit code other than 0.
const exitRestart = 3

// restartForUpdate ends the agent so the service manager starts the newly
// installed version.
func restartForUpdate() {
	os.Exit(exitRestart)
}

// newUpdater sets up updating for the installed agent binary.
func newUpdater(paths identity.Paths, state identity.State, logger *slog.Logger, stderr io.Writer) (*update.Updater, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return nil, err
	}
	var trialArgs []string
	if standard := identity.PathsForThisMachine(); paths != standard {
		trialArgs = []string{"--state-dir", paths.StateDir, "--config-dir", paths.ConfigDir}
	}
	return update.New(update.Options{
		Executable: executable, StateDir: paths.StateDir, ServerURL: state.ServerURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{TLSClientConfig: core.TLSConfig(state.TLSFingerprint)}},
		Version:    buildinfo.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
		RunTrial: func(ctx context.Context, executable string, args []string) error {
			trial := exec.CommandContext(ctx, executable, args...) //nolint:gosec // G204: the verified new build
			trial.Stdout, trial.Stderr = stderr, stderr
			trial.WaitDelay = 10 * time.Second
			return trial.Run()
		},
		TrialArgs: trialArgs, Clock: clock.System{}, Logger: logger,
	}), nil
}

// watchNewVersion is the agent's Observe: for a new version that hasn't
// connected yet, it rolls back unless the agent connects within the time
// allowed (SEC-32).
func watchNewVersion(updater *update.Updater, logger *slog.Logger) func(core.Event) {
	if !updater.Watch() {
		return nil
	}
	connected := make(chan struct{})
	var once sync.Once
	go func() {
		timer := time.NewTimer(update.ConnectWithin)
		defer timer.Stop()
		select {
		case <-connected:
		case <-timer.C:
			logger.Error("this new version couldn't connect; putting the previous version back", "within", update.ConnectWithin)
			if err := updater.Rollback(); err != nil {
				logger.Error("could not put the previous version back", "error", err)
				return
			}
			restartForUpdate()
		}
	}()
	return func(event core.Event) {
		if event == core.EventConnected {
			once.Do(func() { close(connected) })
		}
	}
}

// runTrial is `run --update-trial FILE`: a new build trying to connect.
// Once its handshake succeeds it writes FILE and exits, so the running
// agent can install it.
func runTrial(paths identity.Paths, marker string, logger *slog.Logger, stderr io.Writer) int {
	options, err := agentOptions(paths, logger)
	if err != nil {
		return report("run", err, stderr)
	}
	ctx, stop := signalContext()
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	connected := false
	options.Observe = func(event core.Event) {
		if event == core.EventConnected && !connected {
			connected = true
			if err := os.WriteFile(marker, []byte("connected "+buildinfo.Version()+"\n"), 0o600); err != nil {
				logger.Error("could not record the trial", "error", err)
			}
			cancel()
		}
	}
	core.New(options).Run(ctx)
	if !connected {
		return exitFailure
	}
	return exitOK
}

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kardianos/service"

	"github.com/aazam476/sentinel/internal/agent/collectors"
	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/identity"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/setup"
	"github.com/aazam476/sentinel/internal/agent/update"
	"github.com/aazam476/sentinel/internal/shared/buildinfo"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// serviceConfig describes the agent to systemd, launchd or the Windows
// service manager. Arguments tells it to run `sentinel-agent run` with the
// same file locations the install used.
func serviceConfig(paths identity.Paths) *service.Config {
	arguments := []string{"run"}
	if standard := identity.PathsForThisMachine(); paths != standard {
		arguments = append(arguments, "--state-dir", paths.StateDir, "--config-dir", paths.ConfigDir)
	}
	return &service.Config{
		Name:        "sentinel-agent",
		DisplayName: "Sentinel Agent",
		Description: "Connects this machine to its Sentinel server.",
		Arguments:   arguments,
		Option: service.KeyValue{
			"Restart":   "always", // systemd: restart after a crash
			"OnFailure": "restart",
			"KeepAlive": true, // launchd: the same
			"RunAtLoad": true,
		},
	}
}

func setupOptions(paths identity.Paths, stdout io.Writer, dryRun bool) setup.Options {
	var installed setup.Service
	installed, err := service.New(&program{}, serviceConfig(paths))
	if err != nil {
		installed = unavailableService{err: err}
	}
	return setup.Options{
		Paths: paths, Service: installed, Enroll: core.Enroll, Hostname: os.Hostname,
		Version: buildinfo.Version(), Out: stdout, DryRun: dryRun,
	}
}

// runAgent is `sentinel-agent run`: under a service manager it runs as the
// service; in a terminal it runs until Ctrl-C.
func runAgent(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	paths := pathFlags(flags)
	trialMarker := flags.String(update.TrialFlag, "", "run as an update's trial: write to this file once connected, then exit")
	if err := flags.Parse(args); err != nil {
		return exitUsageError
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	if *trialMarker != "" {
		return runTrial(*paths, *trialMarker, logger, stderr)
	}
	agent, err := buildAgent(*paths, logger, stderr)
	if err != nil {
		return report("run", err, stderr)
	}
	if service.Interactive() {
		ctx, stop := signalContext()
		defer stop()
		agent.Run(ctx)
		return exitOK
	}
	installed, err := service.New(&program{agent: agent}, serviceConfig(*paths))
	if err == nil {
		err = installed.Run()
	}
	return report("run", err, stderr)
}

// buildAgent loads everything the agent needs. A missing identity, or a
// policy file that can't be trusted, stops it here (SEC-22).
func buildAgent(paths identity.Paths, logger *slog.Logger, stderr io.Writer) (*core.Agent, error) {
	options, err := agentOptions(paths, logger)
	if err != nil {
		return nil, err
	}
	updater, err := newUpdater(paths, options.State, logger, stderr)
	if err != nil {
		logger.Warn("this agent can't update itself", "error", err)
		return core.New(options), nil
	}
	options.Updater, options.Restart = updater, restartForUpdate
	options.Observe = watchNewVersion(updater, logger)
	return core.New(options), nil
}

// agentOptions loads the agent's identity, key and policy.
func agentOptions(paths identity.Paths, logger *slog.Logger) (core.Options, error) {
	store, err := identity.Open(paths.StateDir)
	if err != nil {
		return core.Options{}, err
	}
	state, err := store.Load()
	if err != nil {
		return core.Options{}, err
	}
	key, err := store.Key()
	if err != nil {
		return core.Options{}, err
	}
	localPolicy, err := policy.Load(paths.PolicyFile())
	if err != nil {
		return core.Options{}, err
	}
	logger.Info("starting", "version", buildinfo.Version(), "server", state.ServerURL, "agent", state.AgentID)
	return core.Options{
		State: state, Key: key, Policy: localPolicy, Collector: collectors.New(collectors.NewHostSource(), logger),
		Clock: clock.System{}, Logger: logger, Version: buildinfo.Version(),
	}, nil
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// program adapts the agent to the service manager, which calls Start and
// Stop. Start must return at once, so the agent runs in a goroutine. For
// install and uninstall, which only control the service, agent is nil.
type program struct {
	agent  *core.Agent
	cancel context.CancelFunc
	done   chan struct{}
}

// Start is called by the service manager when the service starts.
func (p *program) Start(service.Service) error {
	if p.agent == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.done = cancel, make(chan struct{})
	go func() {
		p.agent.Run(ctx)
		close(p.done)
	}()
	return nil
}

// Stop is called by the service manager when the service stops; it waits
// for the agent to disconnect cleanly.
func (p *program) Stop(service.Service) error {
	if p.cancel != nil {
		p.cancel()
		<-p.done
	}
	return nil
}

// unavailableService stands in when this system has no supported service
// manager; every action explains that.
type unavailableService struct {
	err error
}

func (u unavailableService) fail() error {
	return fmt.Errorf("this system has no supported service manager: %w", u.err)
}

func (u unavailableService) Install() error   { return u.fail() }
func (u unavailableService) Uninstall() error { return u.fail() }
func (u unavailableService) Start() error     { return u.fail() }
func (u unavailableService) Stop() error      { return u.fail() }

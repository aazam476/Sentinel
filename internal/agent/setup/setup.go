// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/identity"
)

// Service is the operating system's service entry for the agent.
type Service interface {
	Install() error
	Uninstall() error
	Start() error
	Stop() error
}

// Options are what every step needs.
type Options struct {
	Paths   identity.Paths
	Service Service
	// Enroll is core.Enroll; tests replace it.
	Enroll func(ctx context.Context, options core.EnrollOptions) (identity.State, error)
	// Hostname is os.Hostname; tests replace it.
	Hostname func() (string, error)
	Version  string
	// Autostart starts the tray for everyone who signs in; nil on systems
	// without one.
	Autostart Autostart
	// Out receives a line for every step.
	Out    io.Writer
	DryRun bool
}

// Autostart starts `sentinel-agent tray` for each person who signs in at
// the machine (ADR 0038): a Run entry on Windows, a launch agent on macOS.
type Autostart interface {
	Install() error
	Uninstall() error
}

// Server is what the install command says about the server.
type Server struct {
	URL            string
	Token          string
	Fingerprint    string
	TLSFingerprint string
}

// ErrAlreadyEnrolled means the machine already has an agent identity. It
// is never reassigned.
var ErrAlreadyEnrolled = errors.New("this machine is already enrolled; run `sentinel-agent uninstall` first to enroll it again")

// Enroll registers the agent with the server and saves its identity and a
// starter policy file, without installing the service.
func Enroll(ctx context.Context, options Options, server Server) error {
	if options.DryRun {
		options.say("Would create %s, readable only by root, for the agent's key and identity", options.Paths.StateDir)
		options.say("Would enroll with %s and check its key against %s", server.URL, server.Fingerprint)
		options.say("Would write a starter policy file at %s if there is none", options.Paths.PolicyFile())
		return nil
	}
	store, err := identity.Open(options.Paths.StateDir)
	if err != nil {
		return err
	}
	if _, err := store.Load(); err == nil {
		return ErrAlreadyEnrolled
	} else if !errors.Is(err, identity.ErrNotEnrolled) {
		return err
	}
	state, err := options.enroll(ctx, store, server)
	if err != nil {
		return err
	}
	if err := store.Save(state); err != nil {
		return err
	}
	options.say("Enrolled with %s as agent %s", state.ServerURL, state.AgentID)
	return options.writeStarterPolicy()
}

func (options Options) enroll(ctx context.Context, store *identity.Store, server Server) (identity.State, error) {
	key, err := store.Key()
	if err != nil {
		return identity.State{}, err
	}
	hostname, err := options.Hostname()
	if err != nil {
		return identity.State{}, fmt.Errorf("read this machine's name: %w", err)
	}
	options.say("Enrolling with %s ...", server.URL)
	return options.Enroll(ctx, core.EnrollOptions{
		ServerURL: server.URL, Token: server.Token, Fingerprint: server.Fingerprint, TLSFingerprint: server.TLSFingerprint,
		Key: key, Hostname: hostname, OS: runtime.GOOS, Arch: runtime.GOARCH, AgentVersion: options.Version,
	})
}

// Install enrolls the agent, then installs and starts the service.
func Install(ctx context.Context, options Options, server Server) error {
	if err := Enroll(ctx, options, server); err != nil {
		return err
	}
	if options.DryRun {
		options.say("Would install the Sentinel Agent service and start it")
		if options.Autostart != nil {
			options.say("Would start the Sentinel tray for everyone who signs in")
		}
		return nil
	}
	if err := options.Service.Install(); err != nil {
		return fmt.Errorf("install the service: %w", err)
	}
	if err := options.Service.Start(); err != nil {
		return fmt.Errorf("start the service: %w", err)
	}
	options.say("Installed and started the Sentinel Agent service")
	options.installAutostart()
	return nil
}

// installAutostart starts the tray at sign-in. The agent works without
// it, so a failure is reported, not returned.
func (options Options) installAutostart() {
	if options.Autostart == nil {
		return
	}
	if err := options.Autostart.Install(); err != nil {
		options.say("The Sentinel tray won't start at sign-in (%v); run `sentinel-agent tray` to show it", err)
		return
	}
	options.say("The Sentinel icon will show in the menu bar or notification area for everyone who signs in")
}

// Uninstall stops and removes the service and deletes the agent's state
// and configuration. Steps that find nothing to do are skipped, so it can
// be run again after a partial failure.
func Uninstall(options Options) error {
	if options.DryRun {
		options.say("Would stop and remove the Sentinel Agent service")
		options.say("Would delete %s and %s", options.Paths.StateDir, options.Paths.ConfigDir)
		return nil
	}
	_ = options.Service.Stop() // not running is fine; Uninstall reports real problems
	if err := options.Service.Uninstall(); err != nil {
		options.say("The service wasn't removed (%v); it may not have been installed", err)
	} else {
		options.say("Removed the Sentinel Agent service")
	}
	if options.Autostart != nil {
		if err := options.Autostart.Uninstall(); err != nil {
			options.say("The tray's sign-in entry wasn't removed (%v)", err)
		} else {
			options.say("The Sentinel tray no longer starts at sign-in")
		}
	}
	for _, dir := range []string{options.Paths.StateDir, options.Paths.ConfigDir} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("delete %s: %w", dir, err)
		}
		options.say("Deleted %s", dir)
	}
	return nil
}

// starterPolicy is written on first enrollment so admins find the file and
// what it can do. It switches nothing off. Its examples are for the system
// it is written on, so each one works once uncommented.
func starterPolicy(goos string) string {
	folders := `["/srv", "/home"]`
	if goos == "windows" {
		folders = `['D:\Shares', 'C:\Users']`
	}
	text := starterPolicyHead + `
# Limit the file manager to these folders and what is inside them; without
# this it can reach the whole file system.
# [files]
# allowed_paths = ` + folders + "\n"
	if goos == "linux" {
		text += starterPolicyX11
	}
	return text
}

const starterPolicyHead = `# Sentinel agent policy (SEC-22). The server can't change this file.
#
# Every capability is allowed unless switched off here. To switch one off,
# remove the # and set it to false, then restart the agent.

# Ask the person at this machine to allow each remote desktop session
# (SEC-23), and show a notice on screen while anyone is connected (SEC-24).
# These must stay above the first [section].
# desktop_requires_consent = true
# show_session_indicator   = true

[capabilities]
# terminal = false
# desktop  = false
# files    = false
# scripts  = false
# docker   = false
# storage  = false
# packages = false
# power    = false

# Where Docker (or Podman) listens and where stacks are kept, if not the
# usual places.
# [docker]
# socket     = "unix:///run/podman/podman.sock"
# stacks_dir = "/opt/sentinel/stacks"
`

const starterPolicyX11 = `
# Remote desktop shares the active X11 session. Name a display here where
# it can't be found (no systemd), or share a test pattern instead.
# [desktop]
# display      = ":0"
# xauthority   = "/home/kiosk/.Xauthority"
# test_pattern = true
`

func (options Options) writeStarterPolicy() error {
	path := options.Paths.PolicyFile()
	_, err := os.Stat(path)
	if err == nil {
		return nil // keep the admin's file
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check for a policy file at %s: %w", path, err)
	}
	if err := os.MkdirAll(options.Paths.ConfigDir, 0o755); err != nil { //nolint:gosec // G301: the policy is readable by all, writable by root only
		return fmt.Errorf("create %s: %w", options.Paths.ConfigDir, err)
	}
	if err := os.WriteFile(path, []byte(starterPolicy(runtime.GOOS)), 0o644); err != nil { //nolint:gosec // G306: readable by all, writable by root only
		return fmt.Errorf("write %s: %w", path, err)
	}
	options.say("Wrote a starter policy file at %s; edit it to switch capabilities off", path)
	return nil
}

func (options Options) say(format string, args ...any) {
	_, _ = fmt.Fprintf(options.Out, format+"\n", args...) // progress output; nothing to do if it fails
}

// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Errors from Service. They are never reassigned.
var (
	// ErrUnsupported: this machine has no package manager the agent knows.
	ErrUnsupported = errors.New("this machine has no package manager Sentinel supports")
	// ErrUnknownManager: the machine doesn't have the manager asked for.
	ErrUnknownManager = errors.New("this machine doesn't have that package manager")
	// ErrInvalidID: an update ID can't be passed to the package manager.
	ErrInvalidID = errors.New("that isn't a valid update")
	// ErrNotSelectable: the manager only updates everything at once.
	ErrNotSelectable = errors.New("this package manager only installs every update at once")
)

// maxOutput is how much of a manager's output the result keeps: the end,
// where the errors are.
const maxOutput = 64 << 10

// maxID is the longest update ID Apply takes.
const maxID = 256

// Manager is one package manager.
type Manager interface {
	// Name is the manager's name in the protocol (apt, winget, ...).
	Name() string
	// Selectable says whether Apply can install some updates only.
	Selectable() bool
	// List refreshes the manager's catalog and lists its updates.
	List(ctx context.Context) ([]protocol.PackageUpdate, error)
	// Apply installs the updates with these IDs, or all when ids is empty,
	// and returns what the manager printed.
	Apply(ctx context.Context, ids []string) ([]byte, error)
	// validID checks one ID before it reaches the manager's program.
	validID(id string) bool
}

// Host is what Detect needs to know about the machine.
type Host struct {
	// GOOS is runtime.GOOS.
	GOOS   string
	Runner command.Runner
	// Find returns a program's path, or "" when the machine doesn't have it.
	Find func(name string) string
	// Exists says whether a file exists (/var/run/reboot-required).
	Exists func(path string) bool
	// Owner returns the user who owns a file: Homebrew refuses to run as
	// root, so it runs as whoever installed it.
	Owner func(path string) (string, error)
}

// Service is the machine's package managers.
type Service struct {
	host     Host
	managers []Manager
}

// New finds the machine's package managers.
func New(host Host) *Service {
	return &Service{host: host, managers: detect(host)}
}

// detect returns the managers the machine has, in the order shown.
func detect(host Host) []Manager {
	switch host.GOOS {
	case "linux":
		return linuxManagers(host)
	case "darwin":
		return darwinManagers(host)
	case "windows":
		return windowsManagers(host)
	default:
		return nil
	}
}

// Supported says whether the machine has any package manager Sentinel
// knows, so the UI can hide the Updates tab when it doesn't.
func (s *Service) Supported() bool { return len(s.managers) > 0 }

// List asks every manager for its updates. A manager that fails is listed
// with its error, so the others still show.
func (s *Service) List(ctx context.Context) (protocol.PackageUpdatesPayload, error) {
	if len(s.managers) == 0 {
		return protocol.PackageUpdatesPayload{}, ErrUnsupported
	}
	payload := protocol.PackageUpdatesPayload{Sources: []protocol.PackageSource{}, RestartRequired: s.restartRequired(ctx)}
	for _, manager := range s.managers {
		source := protocol.PackageSource{Manager: manager.Name(), Updates: []protocol.PackageUpdate{}, Selectable: manager.Selectable()}
		updates, err := manager.List(ctx)
		if err != nil {
			source.Error = err.Error()
		} else {
			source.Updates = updates
		}
		payload.Sources = append(payload.Sources, source)
	}
	return payload, nil
}

// Apply installs updates from one manager.
func (s *Service) Apply(ctx context.Context, request protocol.PackageApplyPayload) (protocol.PackageApplyResult, error) {
	manager, err := s.manager(request.Manager)
	if err != nil {
		return protocol.PackageApplyResult{}, err
	}
	if len(request.IDs) > 0 && !manager.Selectable() {
		return protocol.PackageApplyResult{}, ErrNotSelectable
	}
	for _, id := range request.IDs {
		if len(id) > maxID || !manager.validID(id) {
			return protocol.PackageApplyResult{}, fmt.Errorf("%q: %w", id, ErrInvalidID)
		}
	}
	output, err := manager.Apply(ctx, request.IDs)
	result := protocol.PackageApplyResult{Output: tail(output), RestartRequired: s.restartRequired(ctx)}
	if err != nil {
		return result, fmt.Errorf("install %s updates: %w", manager.Name(), err)
	}
	return result, nil
}

func (s *Service) manager(name string) (Manager, error) {
	for _, manager := range s.managers {
		if manager.Name() == name {
			return manager, nil
		}
	}
	if len(s.managers) == 0 {
		return nil, ErrUnsupported
	}
	return nil, fmt.Errorf("%q: %w", name, ErrUnknownManager)
}

// restartRequired asks the operating system whether updates wait for a
// restart.
func (s *Service) restartRequired(ctx context.Context) bool {
	switch s.host.GOOS {
	case "linux":
		return s.host.Exists("/var/run/reboot-required")
	case "windows":
		output, err := s.host.Runner.Run(ctx, powershell, powershellArgs(rebootScript)...)
		return err == nil && strings.TrimSpace(string(output)) == "True"
	default:
		return false
	}
}

// tail keeps the end of a manager's output.
func tail(output []byte) string {
	if len(output) > maxOutput {
		output = output[len(output)-maxOutput:]
	}
	return strings.ToValidUTF8(string(output), "�")
}

// packageName is a package name on the Linux managers and Homebrew: no
// leading "-", which would read as an option, and no spaces.
var packageName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.+:@/~-]*$`)

// fields splits a line on runs of spaces.
func fields(line string) []string { return strings.Fields(line) }

// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Launchd manages the system domain's launchd jobs through launchctl. The
// agent runs as root, so these are the machine's daemons, not any user's
// agents.
type Launchd struct {
	runner command.Runner
}

// NewLaunchd returns a Launchd that runs launchctl with runner.
func NewLaunchd(runner command.Runner) Launchd { return Launchd{runner: runner} }

// List combines `launchctl list` (loaded jobs, with their PID and last exit
// status) with `launchctl print-disabled system` (jobs switched off).
func (l Launchd) List(ctx context.Context) (protocol.ServiceListPayload, error) {
	loaded, err := l.runner.Run(ctx, "launchctl", "list")
	if err != nil {
		return protocol.ServiceListPayload{}, fmt.Errorf("list jobs: %w", unsupportedIfMissing(err))
	}
	disabledOutput, err := l.runner.Run(ctx, "launchctl", "print-disabled", "system")
	if err != nil {
		return protocol.ServiceListPayload{}, fmt.Errorf("list disabled jobs: %w", unsupportedIfMissing(err))
	}
	disabled := ParseDisabled(disabledOutput)
	jobs := ParseLaunchctlList(loaded)
	for i := range jobs {
		if disabled[jobs[i].Name] {
			jobs[i].Startup = protocol.StartupDisabled
		}
	}
	slices.SortFunc(jobs, func(a, b protocol.Service) int { return strings.Compare(a.Name, b.Name) })
	return protocol.ServiceListPayload{Manager: "launchd", Services: jobs}, nil
}

// Do runs launchctl for the action. Stopping sends SIGTERM: launchd starts
// a job marked KeepAlive again, as it would after any exit.
func (l Launchd) Do(ctx context.Context, action Action, name string) error {
	if err := ValidateName("darwin", name); err != nil {
		return err
	}
	target := "system/" + name
	var args []string
	switch action {
	case Start:
		args = []string{"kickstart", target}
	case Stop:
		args = []string{"kill", "SIGTERM", target}
	case Restart:
		args = []string{"kickstart", "-k", target}
	case Enable, Disable:
		args = []string{string(action), target}
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
	if _, err := l.runner.Run(ctx, "launchctl", args...); err != nil {
		if errors.Is(err, command.ErrNotFound) {
			return fmt.Errorf("launchctl: %w", unsupportedIfMissing(err))
		}
		if strings.Contains(err.Error(), "Could not find service") || strings.Contains(err.Error(), "No such process") {
			return fmt.Errorf("%s: %w", name, ErrNotFound)
		}
		return fmt.Errorf("launchctl %s: %w", action, unsupportedIfMissing(err))
	}
	return nil
}

// ParseLaunchctlList reads `launchctl list`: a header, then
// "PID<tab>Status<tab>Label" per job. PID is "-" for a job that isn't
// running; Status is its last exit status (negative: killed by a signal).
func ParseLaunchctlList(output []byte) []protocol.Service {
	var jobs []protocol.Service
	for fields := range lines(output) {
		if len(fields) != 3 || fields[0] == "PID" || ValidateName("darwin", fields[2]) != nil {
			continue
		}
		job := protocol.Service{Name: fields[2], State: protocol.ServiceStopped, Startup: protocol.StartupAutomatic}
		status, statusErr := strconv.Atoi(fields[1])
		switch _, pidErr := strconv.Atoi(fields[0]); {
		case pidErr == nil:
			job.State = protocol.ServiceRunning
		case fields[0] != "-" || (fields[1] != "-" && statusErr != nil):
			job.State = protocol.ServiceUnknown
		case statusErr == nil && status != 0:
			job.State = protocol.ServiceFailed
		}
		jobs = append(jobs, job)
	}
	return jobs
}

// ParseDisabled reads `launchctl print-disabled system`: lines such as
// `"com.apple.ftpd" => disabled` (or "=> true" before macOS 10.15). It
// returns the labels that are disabled.
func ParseDisabled(output []byte) map[string]bool {
	disabled := map[string]bool{}
	for fields := range lines(output) {
		if len(fields) != 3 || fields[1] != "=>" {
			continue
		}
		label := strings.Trim(fields[0], `"`)
		if fields[2] == "disabled" || fields[2] == "true" {
			disabled[label] = true
		}
	}
	return disabled
}

// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// Errors from Manager. They are never reassigned.
var (
	// ErrUnsupported: this machine has no service manager the agent knows.
	ErrUnsupported = errors.New("this machine's service manager isn't supported")
	// ErrInvalidName: the name can't be a service on this machine.
	ErrInvalidName = errors.New("that isn't a valid service name")
	// ErrNotFound: no service has that name.
	ErrNotFound = errors.New("there is no service with that name")
	// ErrPowerService: the service reboots or powers off the machine; the
	// power actions do that instead.
	ErrPowerService = errors.New("that service restarts or shuts down the machine; use the power actions instead")
)

// Action is something to do to a service.
type Action string

// The actions, named as in the protocol (service.start and so on).
const (
	Start   Action = "start"
	Stop    Action = "stop"
	Restart Action = "restart"
	Enable  Action = "enable"
	Disable Action = "disable"
)

// Actions lists every action.
func Actions() []Action { return []Action{Start, Stop, Restart, Enable, Disable} }

// Manager lists and controls services.
type Manager interface {
	List(ctx context.Context) (protocol.ServiceListPayload, error)
	Do(ctx context.Context, action Action, name string) error
}

// Clock makes timers; Windows uses it to wait for a service to stop.
type Clock interface {
	NewTimer(duration time.Duration) clock.Timer
}

// New returns the Manager for an operating system (runtime.GOOS).
func New(goos string, runner command.Runner, clock Clock) Manager {
	switch goos {
	case "linux":
		return Systemd{runner: runner}
	case "darwin":
		return Launchd{runner: runner}
	case "windows":
		return newSCM(clock)
	default:
		return unsupported{}
	}
}

// ValidateName checks a service name the way the operating system's
// service manager would, before it is passed to any program.
func ValidateName(goos, name string) error {
	var pattern *regexp.Regexp
	switch goos {
	case "linux":
		pattern = systemdName
	case "darwin":
		pattern = launchdLabel
	case "windows":
		pattern = windowsName
	default:
		return ErrUnsupported
	}
	if len(name) > 256 || !pattern.MatchString(name) {
		return fmt.Errorf("%q: %w", name, ErrInvalidName)
	}
	return nil
}

// Patterns for valid names. None allows a leading "-" (it would read as an
// option) or a quote (logs put names into query expressions).
var (
	// systemdName only allows services: starting "reboot.target" would
	// get around the power capability (SEC-22).
	systemdName = regexp.MustCompile(`^[A-Za-z0-9:_.\\@][A-Za-z0-9:_.\\@-]*\.service$`)
	// launchdLabel is a reverse-DNS label such as com.openssh.sshd.
	launchdLabel = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	// windowsName allows the characters Windows service names use.
	windowsName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9 _.$-]*$`)
)

type unsupported struct{}

func (unsupported) List(context.Context) (protocol.ServiceListPayload, error) {
	return protocol.ServiceListPayload{}, ErrUnsupported
}

func (unsupported) Do(context.Context, Action, string) error { return ErrUnsupported }

// unsupportedIfMissing turns a missing program into ErrUnsupported.
func unsupportedIfMissing(err error) error {
	if errors.Is(err, command.ErrNotFound) {
		return fmt.Errorf("%w: %w", ErrUnsupported, err)
	}
	return err
}

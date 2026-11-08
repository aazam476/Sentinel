// SPDX-License-Identifier: Apache-2.0

package power

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// Delay is how long after answering the agent waits before rebooting or
// shutting down, so its answer reaches the server first.
const Delay = 2 * time.Second

// ErrUnsupported means the agent doesn't know how to do it on this system.
var ErrUnsupported = errors.New("the agent can't reboot or shut down this kind of machine")

// Action is reboot or shutdown.
type Action string

// The actions.
const (
	Reboot   Action = "reboot"
	Shutdown Action = "shutdown"
)

// Clock makes timers.
type Clock interface {
	NewTimer(duration time.Duration) clock.Timer
}

// Controller reboots and shuts down the machine.
type Controller struct {
	goos   string
	runner command.Runner
	clock  Clock
}

// New returns a Controller for an operating system (runtime.GOOS).
func New(goos string, runner command.Runner, clock Clock) Controller {
	return Controller{goos: goos, runner: runner, clock: clock}
}

// Schedule checks that the action can be done here and starts it after
// Delay. The returned channel gets the command's result; callers may
// ignore it, since a working reboot ends the agent before it can report.
func (c Controller) Schedule(action Action) (<-chan error, error) {
	program, args, err := c.commandFor(action)
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	timer := c.clock.NewTimer(Delay)
	go func() {
		<-timer.C()
		// Not tied to the request: the request is over by now.
		_, err := c.runner.Run(context.Background(), program, args...)
		done <- err
	}()
	return done, nil
}

// commandFor is the shutdown command for the system. Each one acts at once;
// the delay is the agent's own.
func (c Controller) commandFor(action Action) (string, []string, error) {
	if action != Reboot && action != Shutdown {
		return "", nil, fmt.Errorf("unknown power action %q", action)
	}
	reboot := action == Reboot
	switch c.goos {
	case "linux":
		return "shutdown", []string{map[bool]string{true: "-r", false: "-P"}[reboot], "now"}, nil
	case "darwin":
		return "shutdown", []string{map[bool]string{true: "-r", false: "-h"}[reboot], "now"}, nil
	case "windows":
		return "shutdown", []string{map[bool]string{true: "/r", false: "/s"}[reboot], "/t", "0"}, nil
	default:
		return "", nil, ErrUnsupported
	}
}

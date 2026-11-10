// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// consentTimeout is how long the person at the machine has to answer:
// less than the server waits for the session to start.
const consentTimeout = 25 * time.Second

// showTimeout bounds putting the indicator on the screen.
const showTimeout = 5 * time.Second

// Notifier reaches the person at the machine: it asks them to allow a
// remote desktop session (SEC-23) and shows them a notice while remote
// sessions are open (SEC-24).
type Notifier interface {
	Ask(ctx context.Context, question string) (bool, error)
	Show(ctx context.Context, text string) (io.Closer, error)
}

// askConsent asks the person at the machine to allow the session, when
// the local policy wants that. A machine that can't ask refuses.
func (c *connection) askConsent(ctx context.Context, request protocol.DesktopStartPayload) *protocol.Error {
	options := c.agent.options
	if !options.Policy.DesktopRequiresConsent {
		return nil
	}
	if options.Notifier == nil {
		return consentRefusal("This machine asks the person at it before remote desktop, and can't ask on this system.")
	}
	ctx, cancel := clock.WithTimeout(ctx, options.Clock, consentTimeout)
	defer cancel()
	allowed, err := options.Notifier.Ask(ctx, consentQuestion(request))
	switch {
	case err != nil && errors.Is(context.Cause(ctx), clock.ErrTimeout):
		return consentRefusal("The person at the machine didn't answer in time.")
	case err != nil:
		options.Logger.Info("couldn't ask to allow a remote desktop session", "error", err)
		return consentRefusal("No one at the machine could be asked to allow remote desktop.")
	case !allowed:
		return consentRefusal("The person at the machine declined remote desktop.")
	}
	options.Logger.Info("the person at the machine allowed a remote desktop session", "user", request.User)
	return nil
}

func consentRefusal(message string) *protocol.Error {
	return &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: message}
}

func consentQuestion(request protocol.DesktopStartPayload) string {
	who := request.User
	if who == "" {
		who = "Someone"
	}
	doing := "see and control"
	if request.ViewOnly {
		doing = "see"
	}
	return fmt.Sprintf("%s wants to %s this screen through Sentinel. Allow it?", who, doing)
}

// activity keeps the session indicator (SEC-24) in step with the remote
// sessions open: shells and desktop sessions.
type activity struct {
	notifier Notifier
	logger   *slog.Logger

	mutex     sync.Mutex
	terminals int
	desktops  []string
	shown     io.Closer
}

// setTerminals records how many shells run.
func (a *activity) setTerminals(running int) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.terminals = running
	a.update()
}

// beginDesktop records a desktop session by user, until end is called.
func (a *activity) beginDesktop(user string) (end func()) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.desktops = append(a.desktops, user)
	a.update()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mutex.Lock()
			defer a.mutex.Unlock()
			for i, name := range a.desktops {
				if name == user {
					a.desktops = append(a.desktops[:i], a.desktops[i+1:]...)
					break
				}
			}
			a.update()
		})
	}
}

// close takes the indicator away.
func (a *activity) close() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.terminals, a.desktops = 0, nil
	a.update()
}

// update shows the indicator with what is open now, or takes it away.
// It holds the lock, so updates happen in order.
func (a *activity) update() {
	if a.shown != nil {
		_ = a.shown.Close() // replaced or gone either way
		a.shown = nil
	}
	text := activityText(a.terminals, a.desktops)
	if text == "" || a.notifier == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
	defer cancel()
	shown, err := a.notifier.Show(ctx, text)
	if err != nil {
		a.logger.Info("a remote session is open, but the indicator can't be shown", "error", err)
		return
	}
	a.shown = shown
}

// activityText describes the open sessions, or is "" when there are none.
func activityText(terminals int, desktops []string) string {
	var parts []string
	if len(desktops) > 0 {
		parts = append(parts, strings.Join(desktops, ", ")+" on this screen")
	}
	switch terminals {
	case 0:
	case 1:
		parts = append(parts, "1 terminal open")
	default:
		parts = append(parts, fmt.Sprintf("%d terminals open", terminals))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Remote session (Sentinel): " + strings.Join(parts, " · ")
}

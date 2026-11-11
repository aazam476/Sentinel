// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
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

// activity keeps the session indicator (SEC-24) and the tray in step with
// the remote sessions open: shells and desktop sessions.
type activity struct {
	// notifier shows the indicator; nil when the local policy doesn't.
	notifier Notifier
	logger   *slog.Logger
	// changed is told after every change, without the lock held.
	changed func()

	mutex     sync.Mutex
	terminals int
	desktops  []desktopSession
	shown     io.Closer
}

// desktopSession is a remote desktop session in progress.
type desktopSession struct {
	id    string
	user  string
	since time.Time
	// stop ends it, telling the viewer why.
	stop func(reason string)
}

// setTerminals records how many shells run.
func (a *activity) setTerminals(running int) {
	a.mutex.Lock()
	a.terminals = running
	a.update()
	a.mutex.Unlock()
	a.changed()
}

// beginDesktop records a desktop session until end is called.
func (a *activity) beginDesktop(session desktopSession) (end func()) {
	a.mutex.Lock()
	a.desktops = append(a.desktops, session)
	a.update()
	a.mutex.Unlock()
	a.changed()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mutex.Lock()
			a.desktops = slices.DeleteFunc(a.desktops, func(open desktopSession) bool { return open.id == session.id })
			a.update()
			a.mutex.Unlock()
			a.changed()
		})
	}
}

// desktopSessions lists the desktop sessions in progress.
func (a *activity) desktopSessions() []desktopSession {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return slices.Clone(a.desktops)
}

// endDesktop ends the desktop session with this ID; it reports whether
// there was one.
func (a *activity) endDesktop(id, reason string) bool {
	for _, session := range a.desktopSessions() {
		if session.id == id {
			session.stop(reason)
			return true
		}
	}
	return false
}

// endDesktops ends every desktop session.
func (a *activity) endDesktops(reason string) {
	for _, session := range a.desktopSessions() {
		session.stop(reason)
	}
}

// close takes the indicator away.
func (a *activity) close() {
	a.mutex.Lock()
	a.terminals, a.desktops = 0, nil
	a.update()
	a.mutex.Unlock()
	a.changed()
}

// update shows the indicator with what is open now, or takes it away.
// It holds the lock, so updates happen in order.
func (a *activity) update() {
	if a.shown != nil {
		_ = a.shown.Close() // replaced or gone either way
		a.shown = nil
	}
	users := make([]string, 0, len(a.desktops))
	for _, session := range a.desktops {
		users = append(users, session.user)
	}
	text := activityText(a.terminals, users)
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

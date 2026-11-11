// SPDX-License-Identifier: Apache-2.0

package core

import (
	"errors"

	"github.com/aazam476/sentinel/internal/agent/tray"
	"github.com/aazam476/sentinel/internal/protocol"
)

// The reasons a viewer is told when the person at the machine stops them.
const (
	pausedMessage = "The person at the machine paused remote access."
	endedMessage  = "The person at the machine ended the session."
)

// errRemoteAccessPaused refuses terminals and desktop sessions while the
// person at the machine has paused remote access. It is never reassigned.
var errRemoteAccessPaused = errors.New("the person at the machine paused remote access")

// errNoSuchSession: EndSession found nothing with that ID. It is never
// reassigned.
var errNoSuchSession = errors.New("no remote session has that ID")

// EndSession ends the remote session with this ID, from the machine's
// tray (ADR 0038).
func (a *Agent) EndSession(id string) error {
	if a.terminals.End(id, endedMessage) || a.activity.endDesktop(id, endedMessage) {
		return nil
	}
	return errNoSuchSession
}

// SetRemoteAccessPaused refuses new terminal and desktop sessions and ends
// the open ones, or allows them again. Monitoring and everything else
// carry on.
func (a *Agent) SetRemoteAccessPaused(paused bool) {
	a.remotePaused.Store(paused)
	if paused {
		a.terminals.CloseAllFor(pausedMessage)
		a.activity.endDesktops(pausedMessage)
	}
	a.publishStatus()
}

// refuseWhilePaused is the refusal for a session asked for while remote
// access is paused, or nil.
func (a *Agent) refuseWhilePaused() *protocol.Error {
	if !a.remotePaused.Load() {
		return nil
	}
	return &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: pausedMessage}
}

// publishStatus tells the trays what the agent is doing now.
func (a *Agent) publishStatus() {
	if a.options.Tray != nil {
		a.options.Tray.Publish(a.trayStatus())
	}
}

// trayStatus is the agent's state as the tray shows it. Sessions are
// listed only when the local policy shows a session indicator (SEC-24).
func (a *Agent) trayStatus() tray.Status {
	policy := a.options.Policy
	status := tray.Status{
		Connected: a.connected.Load(), Server: a.options.State.ServerURL, Version: a.options.Version,
		Paused: a.remotePaused.Load(), ConsentRequired: policy.DesktopRequiresConsent, Sessions: []tray.Session{},
	}
	if !policy.ShowSessionIndicator {
		return status
	}
	for _, session := range a.activity.desktopSessions() {
		status.Sessions = append(status.Sessions, tray.Session{ID: session.id, Kind: tray.KindDesktop, User: session.user, Since: session.since})
	}
	for _, shell := range a.terminals.Sessions() {
		status.Sessions = append(status.Sessions, tray.Session{ID: shell.ID, Kind: tray.KindTerminal, User: shell.User, Since: shell.Started})
	}
	return status
}

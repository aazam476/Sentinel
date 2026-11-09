// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"encoding/json"

	"github.com/aazam476/sentinel/internal/protocol"
)

// startSession gives a session a context that a pause can cancel.
func (a *Agent) startSession(ctx context.Context) (context.Context, context.CancelFunc) {
	a.sessions.Lock()
	defer a.sessions.Unlock()
	sessionCtx, cancel := context.WithCancel(ctx)
	a.cancelSession = cancel
	return sessionCtx, cancel
}

// waitWhilePaused holds the agent off the server while a new version is
// on trial; false means ctx ended.
func (a *Agent) waitWhilePaused(ctx context.Context) bool {
	a.sessions.Lock()
	paused := a.paused
	a.sessions.Unlock()
	if paused == nil {
		return true
	}
	select {
	case <-paused:
		return true
	case <-ctx.Done():
		return false
	}
}

// pause ends the current connection and keeps the agent off the server
// until resume is called, so a new version can connect on trial.
func (a *Agent) pause() (resume func()) {
	a.sessions.Lock()
	defer a.sessions.Unlock()
	paused := make(chan struct{})
	a.paused = paused
	if a.cancelSession != nil {
		a.cancelSession()
	}
	return func() {
		a.sessions.Lock()
		defer a.sessions.Unlock()
		a.paused = nil
		close(paused)
	}
}

// startUpdate answers agent.update at once and updates in the background:
// the outcome is reported later (SEC-31, SEC-32).
func (c *connection) startUpdate(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	a := c.agent
	if a.options.Updater == nil {
		return nil, &protocol.Error{Code: protocol.ErrorUnsupportedPlatform, Message: "This agent can't update itself here."}
	}
	var payload protocol.AgentUpdatePayload
	if err := json.Unmarshal(request.Payload, &payload); err != nil {
		return nil, &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: "The update request is malformed."}
	}
	if !a.updating.CompareAndSwap(false, true) {
		return nil, &protocol.Error{Code: protocol.ErrorConflict, Message: "An update is already in progress."}
	}
	// It starts once the answer is sent, since it may leave the connection,
	// and outlives the request and the connection that asked.
	return afterAnswer(func() { go a.update(context.WithoutCancel(ctx), payload) }), nil
}

func (a *Agent) update(ctx context.Context, request protocol.AgentUpdatePayload) {
	defer a.updating.Store(false)
	a.options.Logger.Info("updating", "to", request.Version)
	err := a.options.Updater.Update(ctx, request, a.pause)
	if err == nil {
		a.options.Logger.Info("the new version connected; restarting as it", "version", request.Version)
		if a.options.Restart != nil {
			a.options.Restart()
		}
		return
	}
	a.options.Logger.Warn("the update failed; carrying on as before", "to", request.Version, "error", err)
	a.options.Updater.Failed(request.Version, err)
	select {
	case a.updateReports <- struct{}{}:
	default:
	}
}

// reportUpdates sends an update's result when there is one: on connecting
// (after an update, or a rollback) and when an update fails.
func (c *connection) reportUpdates(ctx context.Context) {
	updater := c.agent.options.Updater
	if updater == nil {
		return
	}
	for {
		if result := updater.Connected(); result != nil {
			event := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(c.agent.options.Clock.Now()), Action: protocol.ActionAgentUpdateResult}
			if err := c.send(ctx, event, result); err != nil {
				c.agent.options.Logger.Warn("could not report the update's result", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-c.agent.updateReports:
		}
	}
}

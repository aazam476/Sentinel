// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// desktopInputBuffer is how many of the browser's messages wait for a
// session that is busy encoding a frame.
const desktopInputBuffer = 256

// desktopCheckInterval is how often the agent looks again for a desktop
// to share, so the Desktop tab appears soon after someone logs in.
const desktopCheckInterval = 15 * time.Second

// desktopAvailability says whether remote desktop can be used now: the
// local policy allows it and there is a desktop to share.
func (a *Agent) desktopAvailability(ctx context.Context) protocol.DesktopAvailability {
	if !a.options.Policy.Allows(policy.Desktop) {
		return protocol.DesktopAvailability{Reason: policy.DisabledReason}
	}
	if a.remotePaused.Load() {
		return protocol.DesktopAvailability{Reason: pausedMessage}
	}
	return a.options.Desktop.Availability(ctx)
}

// desktopOutput sends a session's messages on its stream.
type desktopOutput struct {
	conn   *connection
	stream protocol.StreamID
}

func (o desktopOutput) Send(ctx context.Context, data []byte) error {
	return streamOutput{conn: o.conn}.SendFrame(ctx, o.stream, data)
}

// startDesktop starts a remote desktop session on the stream (§9.3). It
// runs until the server closes the stream or the connection ends.
func (c *connection) startDesktop(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	if refusal := c.refuseUnlessAllowed(policy.Desktop, "Remote desktop"); refusal != nil {
		return nil, refusal
	}
	if refusal := c.agent.refuseWhilePaused(); refusal != nil {
		return nil, refusal
	}
	var payload protocol.DesktopStartPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	if refusal := c.askConsent(ctx, payload); refusal != nil {
		return nil, refusal
	}
	source, err := c.agent.options.Desktop.Open(ctx)
	if err != nil {
		if errors.Is(err, desktop.ErrUnavailable) {
			return nil, &protocol.Error{Code: protocol.ErrorUnsupportedPlatform, Message: "Remote desktop isn't available: " + err.Error() + "."}
		}
		return nil, &protocol.Error{Code: protocol.ErrorInternal, Message: "The agent couldn't share the desktop: " + err.Error()}
	}
	inbound := make(chan []byte, desktopInputBuffer)
	c.agent.desktopInputs.Store(payload.StreamID, func(data []byte) {
		select {
		case inbound <- data:
		default: // a browser sending faster than the session reads loses input, not the connection
		}
	})
	session := desktop.NewSession(source, desktop.Options{
		ViewOnly: payload.ViewOnly, ICEServers: payload.ICEServers, Clock: c.agent.options.Clock, Logger: c.agent.options.Logger,
	})
	width, height := source.Size()
	c.runDesktop(ctx, payload, source, func(ctx context.Context) error {
		return session.Run(ctx, inbound, desktopOutput{conn: c, stream: payload.StreamID})
	})
	return protocol.DesktopStartedPayload{Source: source.Name(), Width: width, Height: height}, nil
}

// runDesktop runs a started session on its stream until it ends, listed
// for the indicator and the tray, which can stop it.
func (c *connection) runDesktop(ctx context.Context, payload protocol.DesktopStartPayload, source desktop.Source, run func(context.Context) error) {
	var stoppedBecause atomic.Pointer[string]
	var endActivity func()
	listed := make(chan struct{})
	stop := c.follow(ctx, payload.StreamID, func(ctx context.Context) string {
		<-listed
		defer endActivity()
		defer c.agent.desktopInputs.Delete(payload.StreamID)
		defer func() { _ = source.Close() }() // the session is over either way
		err := run(ctx)
		if reason := stoppedBecause.Load(); reason != nil {
			return *reason
		}
		if err != nil {
			return err.Error()
		}
		return "the session ended"
	})
	endActivity = c.agent.activity.beginDesktop(desktopSession{
		id: "desktop-" + strconv.FormatUint(uint64(payload.StreamID), 10), user: payload.User, since: c.agent.options.Clock.Now(),
		stop: func(reason string) { stoppedBecause.Store(&reason); stop() },
	})
	close(listed)
}

// reportDesktop records what the server is told about the desktop, in the
// machine's details; it reports whether that differs from before.
func (c *connection) reportDesktop(availability protocol.DesktopAvailability) bool {
	c.desktopMutex.Lock()
	defer c.desktopMutex.Unlock()
	changed := c.desktopReported != nil && *c.desktopReported != availability
	c.desktopReported = &availability
	return changed
}

// watchDesktop looks for a desktop to share every desktopCheckInterval
// and tells the server when that changes, once it has asked about the
// machine.
func (c *connection) watchDesktop(ctx context.Context, timer clock.Timer) {
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C():
		}
		c.desktopMutex.Lock()
		asked := c.desktopReported != nil
		c.desktopMutex.Unlock()
		if availability := c.agent.desktopAvailability(ctx); asked && c.reportDesktop(availability) {
			event := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(c.agent.options.Clock.Now()), Action: protocol.ActionDesktopAvailability}
			if c.send(ctx, event, availability) != nil {
				return
			}
		}
		timer.Reset(desktopCheckInterval)
		c.agent.observe(EventDesktopChecked)
	}
}

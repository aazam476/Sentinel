// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// fakeUpdater pauses the agent like a trial would, then ends as told.
type fakeUpdater struct {
	// hold, if set, keeps Update waiting before it pauses the agent.
	hold     chan struct{}
	started  chan protocol.AgentUpdatePayload
	proceed  chan error
	failures []string
	report   chan *protocol.AgentUpdateResultPayload
}

func newFakeUpdater() *fakeUpdater {
	return &fakeUpdater{
		started: make(chan protocol.AgentUpdatePayload, 1), proceed: make(chan error),
		report: make(chan *protocol.AgentUpdateResultPayload, 10),
	}
}

func (f *fakeUpdater) Update(_ context.Context, request protocol.AgentUpdatePayload, pause func() func()) error {
	if f.hold != nil {
		<-f.hold
		return errors.New("stopped")
	}
	resume := pause()
	f.started <- request
	err := <-f.proceed
	if err != nil {
		resume()
	}
	return err
}

func (f *fakeUpdater) Failed(version string, cause error) {
	f.failures = append(f.failures, version+": "+cause.Error())
	f.report <- &protocol.AgentUpdateResultPayload{Version: version, Error: cause.Error()}
}

func (f *fakeUpdater) Connected() *protocol.AgentUpdateResultPayload {
	select {
	case report := <-f.report:
		return report
	default:
		return nil
	}
}

func startWithUpdater(t *testing.T, updater *fakeUpdater, restarted chan struct{}) (*server, int64, <-chan hub.Event, *runningAgent) {
	t.Helper()
	s := newServer(t)
	if updater != nil {
		s.machine.Updater = updater
	}
	s.machine.Restart = func() { close(restarted) }
	events := s.subscribe()
	agent := s.startAgent(policy.Policy{})
	waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
	stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	return s, stored.ID, events, agent
}

func updateRequest() protocol.AgentUpdatePayload {
	return protocol.AgentUpdatePayload{Version: "v1.1.0", Path: "/install/updates/v1.1.0/sentinel-agent", Signature: "sig"}
}

func TestUpdate(t *testing.T) {
	t.Run("leaves the server during the trial, then restarts as the new version", func(t *testing.T) {
		updater, restarted := newFakeUpdater(), make(chan struct{})
		s, agentID, events, _ := startWithUpdater(t, updater, restarted)
		if err := s.hub.Request(t.Context(), agentID, protocol.ActionAgentUpdate, updateRequest(), nil); err != nil {
			t.Fatal(err)
		}
		if got := <-updater.started; got != updateRequest() {
			t.Errorf("updating with %+v", got)
		}
		waitForKind(t, events, hub.EventOffline)
		updater.proceed <- nil
		select {
		case <-restarted:
		case <-time.After(10 * time.Second):
			t.Fatal("the agent didn't restart")
		}
	})

	t.Run("comes back and reports a failed update", func(t *testing.T) {
		updater, restarted := newFakeUpdater(), make(chan struct{})
		s, agentID, events, agent := startWithUpdater(t, updater, restarted)
		if err := s.hub.Request(t.Context(), agentID, protocol.ActionAgentUpdate, updateRequest(), nil); err != nil {
			t.Fatal(err)
		}
		<-updater.started
		waitForKind(t, events, hub.EventOffline)
		updater.proceed <- errors.New("the new version failed before connecting")
		// Resumed, the agent waits its retry delay, then reconnects.
		agent.expect(t, core.EventWaiting)
		agent.clock.Advance(time.Minute)
		select {
		case result := <-s.updateResults:
			if result.Version != "v1.1.0" || result.OK || result.Error != "the new version failed before connecting" {
				t.Errorf("reported %+v", result)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the failure was never reported")
		}
	})

	t.Run("refuses a second update while one runs, and a malformed one", func(t *testing.T) {
		updater := newFakeUpdater()
		updater.hold = make(chan struct{})
		s, agentID, _, _ := startWithUpdater(t, updater, make(chan struct{}))
		if err := s.hub.Request(t.Context(), agentID, protocol.ActionAgentUpdate, updateRequest(), nil); err != nil {
			t.Fatal(err)
		}
		var refusal *protocol.Error
		err := s.hub.Request(t.Context(), agentID, protocol.ActionAgentUpdate, updateRequest(), nil)
		if !errors.As(err, &refusal) || refusal.Code != protocol.ErrorConflict {
			t.Errorf("got %v", err)
		}
		err = s.hub.Request(t.Context(), agentID, protocol.ActionAgentUpdate, "not an update", nil)
		if !errors.As(err, &refusal) || refusal.Code != protocol.ErrorInvalidRequest {
			t.Errorf("got %v", err)
		}
		close(updater.hold)
	})

	t.Run("refuses updates when it can't update itself", func(t *testing.T) {
		s, agentID, _, _ := startWithUpdater(t, nil, make(chan struct{}))
		var refusal *protocol.Error
		err := s.hub.Request(t.Context(), agentID, protocol.ActionAgentUpdate, updateRequest(), nil)
		if !errors.As(err, &refusal) || refusal.Code != protocol.ErrorUnsupportedPlatform {
			t.Errorf("got %v", err)
		}
	})
}

// waitForKind waits for an event of a kind from any agent.
func waitForKind(t *testing.T, events <-chan hub.Event, kind hub.EventKind) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Kind == kind {
				return
			}
		case <-timeout:
			t.Fatalf("no %s event", kind)
		}
	}
}

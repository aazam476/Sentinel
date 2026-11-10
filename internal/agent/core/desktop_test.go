// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// brokenDesktop says a desktop is there, then fails to open it.
type brokenDesktop struct{}

func (brokenDesktop) Availability(context.Context) protocol.DesktopAvailability {
	return protocol.DesktopAvailability{Available: true}
}

func (brokenDesktop) Open(context.Context) (desktop.Source, error) {
	return nil, errors.New("the display server hung up")
}

// switchingDesktop is a desktop someone can log in to.
type switchingDesktop struct {
	mutex     sync.Mutex
	available bool
}

func (d *switchingDesktop) set(available bool) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.available = available
}

func (d *switchingDesktop) Availability(context.Context) protocol.DesktopAvailability {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.available {
		return protocol.DesktopAvailability{Available: true}
	}
	return protocol.DesktopAvailability{Reason: "No one is logged in to a graphical session."}
}

func (*switchingDesktop) Open(context.Context) (desktop.Source, error) {
	return nil, desktop.ErrUnavailable
}

// openDesktop starts a desktop session from the server side, as the web
// UI's handler does.
func (s *server) openDesktop(t *testing.T, agentID string) (*hub.Stream, protocol.DesktopStartedPayload, error) {
	t.Helper()
	stored, err := s.fleet.AgentForHandshake(t.Context(), agentID)
	if err != nil {
		t.Fatal(err)
	}
	var started protocol.DesktopStartedPayload
	stream, err := s.hub.OpenStream(t.Context(), stored.ID, protocol.ActionDesktopSessionStart, func(id protocol.StreamID) any {
		return protocol.DesktopStartPayload{StreamID: id}
	}, &started)
	return stream, started, err
}

func (s *server) desktopAvailability(t *testing.T, agentID string) *protocol.DesktopAvailability {
	t.Helper()
	stored, err := s.fleet.AgentForHandshake(t.Context(), agentID)
	if err != nil {
		t.Fatal(err)
	}
	return stored.SystemInfo.Desktop
}

func TestDesktop(t *testing.T) {
	t.Run("shares the test pattern, sending the screen on the stream once asked", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		localPolicy, err := policy.Parse("[desktop]\ntest_pattern = true\n")
		if err != nil {
			t.Fatal(err)
		}
		agent := s.startAgent(localPolicy)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		if got := s.desktopAvailability(t, agent.state.AgentID); got == nil || !got.Available {
			t.Fatalf("availability = %+v, want available", got)
		}

		stream, started, err := s.openDesktop(t, agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		if started != (protocol.DesktopStartedPayload{Source: "test pattern", Width: 1280, Height: 720}) {
			t.Errorf("started = %+v", started)
		}
		if err := stream.Write(t.Context(), append([]byte{protocol.DesktopControl}, `{"type":"fallback"}`...)); err != nil {
			t.Fatal(err)
		}
		// The first update over the stream is the screen's size.
		deadline := time.After(5 * time.Second)
		for {
			agent.clock.Advance(100 * time.Millisecond)
			select {
			case data := <-stream.Frames():
				if data[0] != protocol.DesktopFrame {
					t.Fatalf("first message = %q, want a frame", data)
				}
				update, err := protocol.DecodeScreenUpdate(data[1:])
				if err != nil || update.Kind != protocol.FrameScreen || update.Width != 1280 {
					t.Fatalf("update = %+v (%v)", update, err)
				}
				stream.Close(t.Context(), "closed by the user")
				return
			case <-deadline:
				t.Fatal("no frame over the stream")
			case <-time.After(10 * time.Millisecond):
			}
		}
	})

	t.Run("tells the server when someone logs in and out", func(t *testing.T) {
		s := newServer(t)
		screen := &switchingDesktop{}
		s.machine.Desktop = screen
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		if got := s.desktopAvailability(t, agent.state.AgentID); got == nil || got.Available {
			t.Fatalf("availability = %+v, want none", got)
		}
		for _, available := range []bool{true, false} {
			screen.set(available)
			agent.clock.Advance(15 * time.Second)
			agent.expect(t, core.EventDesktopChecked)
			waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
			if got := s.desktopAvailability(t, agent.state.AgentID); got == nil || got.Available != available {
				t.Errorf("availability = %+v, want available %v", got, available)
			}
		}
	})

	t.Run("SEC-22: refuses a desktop the local policy switches off", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		localPolicy, err := policy.Parse("[capabilities]\ndesktop = false\n[desktop]\ntest_pattern = true\n")
		if err != nil {
			t.Fatal(err)
		}
		agent := s.startAgent(localPolicy)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		if got := s.desktopAvailability(t, agent.state.AgentID); got == nil || got.Available || got.Reason != policy.DisabledReason {
			t.Errorf("availability = %+v, want the policy's reason", got)
		}
		_, _, err = s.openDesktop(t, agent.state.AgentID)
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied {
			t.Errorf("err = %v, want policy_denied", err)
		}
	})

	t.Run("says when there is no desktop to share", func(t *testing.T) {
		s := newServer(t)
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		if got := s.desktopAvailability(t, agent.state.AgentID); got == nil || got.Available || got.Reason == "" {
			t.Errorf("availability = %+v, want a reason", got)
		}
		_, _, err := s.openDesktop(t, agent.state.AgentID)
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorUnsupportedPlatform {
			t.Errorf("err = %v, want unsupported_platform", err)
		}
	})

	t.Run("reports a desktop that fails to open", func(t *testing.T) {
		s := newServer(t)
		s.machine.Desktop = brokenDesktop{}
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		_, _, err := s.openDesktop(t, agent.state.AgentID)
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorInternal {
			t.Errorf("err = %v, want internal", err)
		}
	})
}

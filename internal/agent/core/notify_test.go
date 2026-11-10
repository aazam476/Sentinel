// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// fakeNotifier stands in for the person at the machine.
type fakeNotifier struct {
	mutex     sync.Mutex
	answer    bool
	err       error
	noAnswer  bool
	questions []string
	// shown receives each indicator text, and "" when one is taken away.
	shown chan string
}

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{shown: make(chan string, 32)} }

func (n *fakeNotifier) Ask(ctx context.Context, question string) (bool, error) {
	n.mutex.Lock()
	n.questions = append(n.questions, question)
	answer, err, noAnswer := n.answer, n.err, n.noAnswer
	n.mutex.Unlock()
	if noAnswer {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return answer, err
}

func (n *fakeNotifier) Show(_ context.Context, text string) (io.Closer, error) {
	n.shown <- text
	return closer{n.shown}, nil
}

type closer struct{ shown chan string }

func (c closer) Close() error {
	c.shown <- ""
	return nil
}

// next waits for the next change to the indicator.
func (n *fakeNotifier) next(t *testing.T) string {
	t.Helper()
	select {
	case text := <-n.shown:
		return text
	case <-time.After(10 * time.Second):
		t.Fatal("the indicator didn't change")
		return ""
	}
}

func desktopAgent(t *testing.T, s *server, policyText string) *runningAgent {
	t.Helper()
	// Top-level keys come before any [table], or TOML puts them in it.
	localPolicy, err := policy.Parse(policyText + "[desktop]\ntest_pattern = true\n")
	if err != nil {
		t.Fatal(err)
	}
	events := s.subscribe()
	agent := s.startAgent(localPolicy)
	waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
	return agent
}

func TestSEC23_ThePersonAtTheMachineAllowsRemoteDesktop(t *testing.T) {
	t.Run("starts the session once allowed, saying who asks", func(t *testing.T) {
		s := newServer(t)
		notifier := newFakeNotifier()
		notifier.answer = true
		s.machine.Notifier = notifier
		agent := desktopAgent(t, s, "desktop_requires_consent = true\n")
		stream, _, err := s.openDesktop(t, agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		stream.Close(t.Context(), "closed by the user")
		if len(notifier.questions) != 1 || !strings.Contains(notifier.questions[0], "admin wants to see and control this screen") {
			t.Errorf("questions = %q", notifier.questions)
		}
	})

	tests := []struct {
		name     string
		set      func(*fakeNotifier)
		wantText string
	}{
		{"refuses when the person declines", func(*fakeNotifier) {}, "declined"},
		{"refuses when no one can be asked", func(n *fakeNotifier) { n.err = errors.New("no display") }, "No one at the machine"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newServer(t)
			notifier := newFakeNotifier()
			test.set(notifier)
			s.machine.Notifier = notifier
			agent := desktopAgent(t, s, "desktop_requires_consent = true\n")
			_, _, err := s.openDesktop(t, agent.state.AgentID)
			var agentErr *protocol.Error
			if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied || !strings.Contains(agentErr.Message, test.wantText) {
				t.Errorf("err = %v, want a refusal saying %q", err, test.wantText)
			}
		})
	}

	t.Run("refuses when no one answers in time", func(t *testing.T) {
		s := newServer(t)
		notifier := newFakeNotifier()
		notifier.noAnswer = true
		s.machine.Notifier = notifier
		agent := desktopAgent(t, s, "desktop_requires_consent = true\n")
		result := make(chan error, 1)
		go func() {
			_, _, err := s.openDesktop(t, agent.state.AgentID)
			result <- err
		}()
		for {
			agent.clock.Advance(5 * time.Second)
			select {
			case err := <-result:
				var agentErr *protocol.Error
				if !errors.As(err, &agentErr) || !strings.Contains(agentErr.Message, "didn't answer") {
					t.Errorf("err = %v, want no answer", err)
				}
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	})

	t.Run("doesn't ask unless the policy says", func(t *testing.T) {
		s := newServer(t)
		notifier := newFakeNotifier()
		s.machine.Notifier = notifier
		agent := desktopAgent(t, s, "")
		if _, _, err := s.openDesktop(t, agent.state.AgentID); err != nil {
			t.Fatal(err)
		}
		if len(notifier.questions) != 0 {
			t.Errorf("asked %q", notifier.questions)
		}
	})
}

func TestSEC24_TheSessionIndicator(t *testing.T) {
	t.Run("shows while a desktop session or a terminal is open", func(t *testing.T) {
		s := newServer(t)
		s.withEchoShells()
		notifier := newFakeNotifier()
		s.machine.Notifier = notifier
		agent := desktopAgent(t, s, "show_session_indicator = true\n")

		desktop, _, err := s.openDesktop(t, agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		if got := notifier.next(t); got != "Remote session (Sentinel): admin on this screen" {
			t.Errorf("indicator = %q", got)
		}
		terminal, _, err := s.openTerminal(t, agent.state.AgentID, "")
		if err != nil {
			t.Fatal(err)
		}
		if got, again := notifier.next(t), notifier.next(t); got != "" || again != "Remote session (Sentinel): admin on this screen · 1 terminal open" {
			t.Errorf("indicator = %q then %q", got, again)
		}
		desktop.Close(t.Context(), "closed by the user")
		if got, again := notifier.next(t), notifier.next(t); got != "" || again != "Remote session (Sentinel): 1 terminal open" {
			t.Errorf("indicator = %q then %q", got, again)
		}
		terminal.Close(t.Context(), "closed by the user")
		if got := notifier.next(t); got != "" {
			t.Errorf("indicator = %q, want it gone", got)
		}
	})

	t.Run("shows nothing unless the policy says", func(t *testing.T) {
		s := newServer(t)
		notifier := newFakeNotifier()
		s.machine.Notifier = notifier
		agent := desktopAgent(t, s, "")
		stream, _, err := s.openDesktop(t, agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		stream.Close(t.Context(), "closed by the user")
		select {
		case text := <-notifier.shown:
			t.Errorf("showed %q", text)
		case <-time.After(100 * time.Millisecond):
		}
	})
}

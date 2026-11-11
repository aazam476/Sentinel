// SPDX-License-Identifier: Apache-2.0

package tray_test

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/tray"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// fakeUI records what the tray would show, and answers questions with
// what the test puts in answers.
type fakeUI struct {
	mutex   sync.Mutex
	models  []tray.Model
	notices []string
	// withdrawn counts questions taken away before an answer.
	withdrawn int
	questions chan string
	answers   chan bool
	updated   chan struct{}
}

func newUI() *fakeUI {
	return &fakeUI{questions: make(chan string, 4), answers: make(chan bool, 4), updated: make(chan struct{}, 64)}
}

func (u *fakeUI) Update(model tray.Model) {
	u.mutex.Lock()
	u.models = append(u.models, model)
	u.mutex.Unlock()
	u.updated <- struct{}{}
}

func (u *fakeUI) Ask(ctx context.Context, question string) (bool, error) {
	u.questions <- question
	select {
	case allowed := <-u.answers:
		return allowed, nil
	case <-ctx.Done():
		u.mutex.Lock()
		u.withdrawn++
		u.mutex.Unlock()
		return false, ctx.Err()
	}
}

func (u *fakeUI) Notify(text string) {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	u.notices = append(u.notices, text)
}

// waitForModel waits until the tray shows a model that matches.
func (u *fakeUI) waitForModel(t *testing.T, matches func(tray.Model) bool) tray.Model {
	t.Helper()
	for {
		u.mutex.Lock()
		if n := len(u.models); n > 0 && matches(u.models[n-1]) {
			model := u.models[n-1]
			u.mutex.Unlock()
			return model
		}
		u.mutex.Unlock()
		select {
		case <-u.updated:
		case <-t.Context().Done():
			t.Fatal("the tray never showed the expected model")
		}
	}
}

// runTray runs RunClient against the harness's socket.
func runTray(t *testing.T, h *serverHarness, clock *fakeclock.Clock) (*fakeUI, chan tray.Action) {
	t.Helper()
	ui, actions := newUI(), make(chan tray.Action, 4)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		tray.RunClient(ctx, tray.ClientOptions{
			Dial: func(ctx context.Context) (net.Conn, error) { return tray.Dial(ctx, h.path) },
			UI:   ui, Clock: clock, Logger: slog.New(slog.DiscardHandler),
		}, actions)
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return ui, actions
}

func TestTheTrayShowsTheAgentAndCarriesThePersonsPicks(t *testing.T) {
	h := newServer(t)
	clock := fakeclock.New(time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC))
	h.server.Publish(*sampleStatus())
	ui, actions := runTray(t, h, clock)

	model := ui.waitForModel(t, func(m tray.Model) bool { return m.Icon == tray.IconActive })
	if model.Tooltip != "Sentinel: 1 remote session open" {
		t.Errorf("tooltip = %q", model.Tooltip)
	}
	waitFor(t, func() bool {
		ui.mutex.Lock()
		defer ui.mutex.Unlock()
		return len(ui.notices) == 1 && ui.notices[0] == "admin is connected to this screen through Sentinel"
	})

	t.Run("SEC-23 answers the agent's question", func(t *testing.T) {
		answered := make(chan bool, 1)
		go func() {
			allowed, _ := h.server.Ask(t.Context(), "admin wants to see this screen through Sentinel. Allow it?")
			answered <- allowed
		}()
		if question := <-ui.questions; !strings.Contains(question, "Allow it?") {
			t.Errorf("question = %q", question)
		}
		ui.answers <- true
		if !<-answered {
			t.Error("Ask = false, want the tray's yes")
		}
	})

	t.Run("takes a question away once the agent stops waiting", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		go func() { _, _ = h.server.Ask(ctx, "Allow it?") }()
		<-ui.questions
		cancel()
		ui.answers <- true // a click after it went away goes nowhere
		h.server.Publish(*sampleStatus())
	})

	t.Run("ends a session and pauses", func(t *testing.T) {
		actions <- tray.Action{Kind: tray.ActionEnd, Session: "s1"}
		h.control.wait(t)
		actions <- tray.Action{Kind: tray.ActionPause}
		h.control.wait(t)
		actions <- tray.Action{Kind: tray.ActionResume}
		h.control.wait(t)
		if h.control.ended[0] != "s1" || len(h.control.paused) != 2 || !h.control.paused[0] || h.control.paused[1] {
			t.Errorf("ended %v, paused %v", h.control.ended, h.control.paused)
		}
	})
}

func TestTheTrayWaitsForTheAgentToStart(t *testing.T) {
	clock := fakeclock.New(time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC))
	h := &serverHarness{control: newControl(), path: newServer(t).path + "-later"}
	ui, actions := runTray(t, h, clock)

	model := ui.waitForModel(t, func(m tray.Model) bool { return m.Icon == tray.IconOff })
	if model.Items[0].Label != "The Sentinel agent isn't running" {
		t.Errorf("model = %+v", model)
	}
	actions <- tray.Action{Kind: tray.ActionPause} // nowhere to send it; dropped

	h.start(t)
	h.server.Publish(tray.Status{Connected: true, Server: "https://sentinel.example.com"})
	if err := clock.WaitForTimers(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(5 * time.Second)
	ui.waitForModel(t, func(m tray.Model) bool { return m.Icon == tray.IconIdle })
}

// fakeAgent is a socket the test answers as the agent.
func fakeAgent(t *testing.T) (path string, accepted <-chan net.Conn) {
	t.Helper()
	dir, err := os.MkdirTemp("", "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path = filepath.Join(dir, "a.sock")
	listener, err := tray.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	conns := make(chan net.Conn, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conns <- conn
		}
	}()
	return path, conns
}

func TestTheTrayGivesUpOnAConnectionThatGoesWrong(t *testing.T) {
	path, accepted := fakeAgent(t)
	clock := fakeclock.New(time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC))
	ui, _ := runTray(t, &serverHarness{path: path}, clock)

	t.Run("takes an open question away when the agent goes", func(t *testing.T) {
		conn := <-accepted
		for _, message := range []tray.Message{
			{Type: tray.TypeEnd, ID: "s1"},   // the tray's own kind of message: ignored
			{Type: tray.TypeAsked, ID: "q0"}, // a question it never had: ignored
			{Type: tray.TypeAsk, ID: "q1", Question: "Allow it?"},
		} {
			line, err := tray.Encode(message)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write(line); err != nil {
				t.Fatal(err)
			}
		}
		<-ui.questions
		line, err := tray.Encode(tray.Message{Type: tray.TypeAsked, ID: "q1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(line); err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { // the question is taken off the screen
			ui.mutex.Lock()
			defer ui.mutex.Unlock()
			return ui.withdrawn > 0
		})
		_ = conn.Close()
		ui.waitForModel(t, func(m tray.Model) bool { return m.Icon == tray.IconOff })
	})

	t.Run("drops an agent that sends nonsense", func(t *testing.T) {
		if err := clock.WaitForTimers(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		clock.Advance(5 * time.Second)
		conn := <-accepted
		defer func() { _ = conn.Close() }()
		if _, err := conn.Write([]byte("nonsense\n")); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Read(make([]byte, 1)); err == nil {
			t.Error("the tray kept a connection that sent nonsense")
		}
	})
}

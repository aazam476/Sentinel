// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/terminal"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// echoShell is a fake shell that answers every line typed with
// "you typed: <line>", and records resizes.
type echoShell struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	sizes  chan [2]uint16
	closed chan struct{}
}

func newEchoShell() *echoShell {
	reader, writer := io.Pipe()
	return &echoShell{reader: reader, writer: writer, sizes: make(chan [2]uint16, 8), closed: make(chan struct{})}
}

func (s *echoShell) Read(buffer []byte) (int, error) { return s.reader.Read(buffer) }

func (s *echoShell) Write(data []byte) (int, error) {
	go func() { _, _ = s.writer.Write(append([]byte("you typed: "), data...)) }() // the session reads it
	return len(data), nil
}

func (s *echoShell) Resize(cols, rows uint16) error {
	s.sizes <- [2]uint16{cols, rows}
	return nil
}

func (s *echoShell) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return s.writer.Close()
}

// withEchoShells makes the server's agents start echo shells, and returns
// the shells as they start.
func (s *server) withEchoShells() <-chan *echoShell {
	started := make(chan *echoShell, 8)
	s.shells = func(uint16, uint16) (terminal.Shell, error) {
		shell := newEchoShell()
		started <- shell
		return shell, nil
	}
	return started
}

// openTerminal opens a terminal on the agent from the server side, as the
// web UI's relay does.
func (s *server) openTerminal(t *testing.T, agentID, resume string) (*hub.Stream, protocol.TerminalOpenedPayload, error) {
	t.Helper()
	stored, err := s.fleet.AgentForHandshake(t.Context(), agentID)
	if err != nil {
		t.Fatal(err)
	}
	var opened protocol.TerminalOpenedPayload
	stream, err := s.hub.OpenStream(t.Context(), stored.ID, protocol.ActionTerminalOpen, func(id protocol.StreamID) any {
		return protocol.TerminalOpenPayload{StreamID: id, Cols: 80, Rows: 24, ResumeSessionID: resume}
	}, &opened)
	return stream, opened, err
}

func TestTerminal(t *testing.T) {
	t.Run("runs a shell for the server: input in, output out, resizes", func(t *testing.T) {
		s := newServer(t)
		shells := s.withEchoShells()
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)

		stream, opened, err := s.openTerminal(t, agent.state.AgentID, "")
		if err != nil || opened.SessionID == "" {
			t.Fatalf("open = %+v (%v)", opened, err)
		}
		shell := <-shells
		if err := stream.Write(t.Context(), []byte("uptime\r")); err != nil {
			t.Fatal(err)
		}
		if got := string(<-stream.Frames()); got != "you typed: uptime\r" {
			t.Errorf("output = %q", got)
		}
		stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.hub.Request(t.Context(), stored.ID, protocol.ActionTerminalResize,
			protocol.TerminalResizePayload{StreamID: stream.ID, Cols: 132, Rows: 43}, nil); err != nil {
			t.Fatal(err)
		}
		if size := <-shell.sizes; size != [2]uint16{132, 43} {
			t.Errorf("resized to %v", size)
		}
		stream.Close(t.Context(), "closed by the user")
		<-shell.closed
	})

	t.Run("SEC-22: refuses a terminal the local policy switches off", func(t *testing.T) {
		s := newServer(t)
		s.withEchoShells()
		events := s.subscribe()
		localPolicy, err := policy.Parse("[capabilities]\nterminal = false\n")
		if err != nil {
			t.Fatal(err)
		}
		agent := s.startAgent(localPolicy)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		_, _, err = s.openTerminal(t, agent.state.AgentID, "")
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied {
			t.Errorf("err = %v, want policy_denied", err)
		}
	})

	t.Run("keeps a shell through the agent's own reconnect, to be resumed", func(t *testing.T) {
		s := newServer(t)
		shells := s.withEchoShells()
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		stream, opened, err := s.openTerminal(t, agent.state.AgentID, "")
		if err != nil {
			t.Fatal(err)
		}
		shell := <-shells
		if err := stream.Write(t.Context(), []byte("before\r")); err != nil {
			t.Fatal(err)
		}
		<-stream.Frames()

		s.proxy.Drop()
		agent.expect(t, core.EventWaiting)
		agent.clock.Advance(2 * time.Second)
		agent.expect(t, core.EventConnected)
		waitFor(t, events, agent.state.AgentID, hub.EventOnline)

		resumed, again, err := s.openTerminal(t, agent.state.AgentID, opened.SessionID)
		if err != nil || !again.Resumed {
			t.Fatalf("resume = %+v (%v)", again, err)
		}
		if got := string(<-resumed.Frames()); got != "you typed: before\r" {
			t.Errorf("replay = %q", got)
		}
		select {
		case <-shell.closed:
			t.Error("the shell ended during the reconnect")
		default:
		}
	})

	t.Run("can't resume a session the agent doesn't have", func(t *testing.T) {
		s := newServer(t)
		s.withEchoShells()
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		_, _, err := s.openTerminal(t, agent.state.AgentID, "01JBX3K5Q7Z8M4N6P8R0T2V4W6")
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorNotFound {
			t.Errorf("err = %v, want not_found", err)
		}
	})

	t.Run("tells the server when the shell exits by itself", func(t *testing.T) {
		s := newServer(t)
		shells := s.withEchoShells()
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		stream, _, err := s.openTerminal(t, agent.state.AgentID, "")
		if err != nil {
			t.Fatal(err)
		}
		_ = (<-shells).Close()
		if reason := stream.Reason(); reason != "the shell exited" {
			t.Errorf("reason = %q", reason)
		}
	})

	t.Run("reports a shell that won't start", func(t *testing.T) {
		s := newServer(t)
		s.shells = func(uint16, uint16) (terminal.Shell, error) { return nil, errors.New("no such file: /bin/zsh") }
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		_, _, err := s.openTerminal(t, agent.state.AgentID, "")
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorInternal {
			t.Errorf("err = %v, want internal", err)
		}
	})

	t.Run("refuses malformed requests and resizes of streams it doesn't have", func(t *testing.T) {
		s := newServer(t)
		s.withEchoShells()
		events := s.subscribe()
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			action  string
			payload any
			code    protocol.ErrorCode
		}{
			{protocol.ActionTerminalOpen, "not an object", protocol.ErrorInvalidRequest},
			{protocol.ActionTerminalResize, "not an object", protocol.ErrorInvalidRequest},
			{protocol.ActionTerminalResize, protocol.TerminalResizePayload{StreamID: 99, Cols: 80, Rows: 24}, protocol.ErrorNotFound},
		}
		for _, c := range cases {
			err := s.hub.Request(t.Context(), stored.ID, c.action, c.payload, nil)
			var agentErr *protocol.Error
			if !errors.As(err, &agentErr) || agentErr.Code != c.code {
				t.Errorf("%s %v: err = %v, want %s", c.action, c.payload, err, c.code)
			}
		}
	})
}

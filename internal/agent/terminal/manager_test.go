// SPDX-License-Identifier: Apache-2.0

package terminal_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/terminal"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// fakeShell is a shell whose output the test writes and whose input the
// test reads.
type fakeShell struct {
	outputReader *io.PipeReader
	outputWriter *io.PipeWriter
	input        chan string
	sizes        chan [2]uint16
	closed       chan struct{}
	once         sync.Once
}

func newFakeShell() *fakeShell {
	reader, writer := io.Pipe()
	return &fakeShell{outputReader: reader, outputWriter: writer, input: make(chan string, 16),
		sizes: make(chan [2]uint16, 16), closed: make(chan struct{})}
}

func (s *fakeShell) Read(buffer []byte) (int, error) { return s.outputReader.Read(buffer) }

func (s *fakeShell) Write(data []byte) (int, error) {
	s.input <- string(data)
	return len(data), nil
}

func (s *fakeShell) Resize(cols, rows uint16) error {
	s.sizes <- [2]uint16{cols, rows}
	return nil
}

func (s *fakeShell) Close() error {
	s.once.Do(func() {
		close(s.closed)
		_ = s.outputWriter.Close()
	})
	return nil
}

// print makes the shell write output.
func (s *fakeShell) print(t *testing.T, text string) {
	t.Helper()
	if _, err := s.outputWriter.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
}

// printAndSettle writes output and then a prompt. The pipe hands the
// prompt over only once the session has finished with the output, so when
// this returns, the output is recorded.
func (s *fakeShell) printAndSettle(t *testing.T, text string) {
	t.Helper()
	s.print(t, text)
	s.print(t, "$ ")
}

// exit makes the shell exit by itself.
func (s *fakeShell) exit() { _ = s.outputWriter.Close() }

// fakeOutput records what the manager sends to the server.
type fakeOutput struct {
	frames chan string
	closed chan string
	fail   bool
}

func newFakeOutput() *fakeOutput {
	return &fakeOutput{frames: make(chan string, 64), closed: make(chan string, 4)}
}

func (o *fakeOutput) SendFrame(_ context.Context, _ protocol.StreamID, data []byte) error {
	if o.fail {
		return errors.New("connection gone")
	}
	o.frames <- string(data)
	return nil
}

func (o *fakeOutput) CloseStream(_ context.Context, _ protocol.StreamID, reason string) {
	o.closed <- reason
}

type fixture struct {
	manager *terminal.Manager
	clock   *fakeclock.Clock
	shells  chan *fakeShell
}

func newFixture() *fixture {
	f := &fixture{clock: fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)), shells: make(chan *fakeShell, 64)}
	start := func(cols, rows uint16) (terminal.Shell, error) {
		shell := newFakeShell()
		shell.sizes <- [2]uint16{cols, rows}
		f.shells <- shell
		return shell, nil
	}
	f.manager = terminal.NewManager(start, f.clock, slog.New(slog.DiscardHandler))
	return f
}

func (f *fixture) open(t *testing.T, stream protocol.StreamID, out terminal.Output) (protocol.TerminalOpenedPayload, *fakeShell) {
	t.Helper()
	opened, err := f.manager.Open(protocol.TerminalOpenPayload{StreamID: stream, Cols: 120, Rows: 40}, out)
	if err != nil {
		t.Fatal(err)
	}
	return opened, <-f.shells
}

func TestManager(t *testing.T) {
	t.Run("starts a shell of the requested size and relays output, input and resizes", func(t *testing.T) {
		f := newFixture()
		out := newFakeOutput()
		opened, shell := f.open(t, 7, out)
		if opened.SessionID == "" || opened.Resumed {
			t.Errorf("opened = %+v", opened)
		}
		if size := <-shell.sizes; size != [2]uint16{120, 40} {
			t.Errorf("started at %v", size)
		}
		shell.print(t, "$ ")
		if got := <-out.frames; got != "$ " {
			t.Errorf("output = %q", got)
		}
		f.manager.Input(7, []byte("ls\r"))
		if got := <-shell.input; got != "ls\r" {
			t.Errorf("input = %q", got)
		}
		if err := f.manager.Resize(protocol.TerminalResizePayload{StreamID: 7, Cols: 80, Rows: 24}); err != nil {
			t.Fatal(err)
		}
		if size := <-shell.sizes; size != [2]uint16{80, 24} {
			t.Errorf("resized to %v", size)
		}
	})

	t.Run("tells the server when the shell exits", func(t *testing.T) {
		f := newFixture()
		out := newFakeOutput()
		_, shell := f.open(t, 7, out)
		shell.exit()
		if reason := <-out.closed; reason != "the shell exited" {
			t.Errorf("reason = %q", reason)
		}
		if err := f.manager.Resize(protocol.TerminalResizePayload{StreamID: 7, Cols: 1, Rows: 1}); !errors.Is(err, terminal.ErrSessionEnded) {
			t.Errorf("resize after exit = %v", err)
		}
	})

	t.Run("ends the shell when the user closes the terminal", func(t *testing.T) {
		f := newFixture()
		_, shell := f.open(t, 7, newFakeOutput())
		f.manager.StreamClosed(protocol.StreamClosePayload{StreamID: 7, Reason: "closed by the user"})
		<-shell.closed
		f.manager.StreamClosed(protocol.StreamClosePayload{StreamID: 7}) // again: nothing to do
		f.manager.Input(7, []byte("ignored"))
	})

	t.Run("keeps a dropped session and replays its recent output on resume", func(t *testing.T) {
		f := newFixture()
		opened, shell := f.open(t, 7, newFakeOutput())
		shell.print(t, "line one\r\n")
		f.manager.StreamClosed(protocol.StreamClosePayload{StreamID: 7, KeepSession: true})
		shell.printAndSettle(t, "line two\r\n") // while nobody watches

		again := newFakeOutput()
		resumed, err := f.manager.Open(protocol.TerminalOpenPayload{StreamID: 8, Cols: 100, Rows: 30, ResumeSessionID: opened.SessionID}, again)
		if err != nil || !resumed.Resumed || resumed.SessionID != opened.SessionID {
			t.Fatalf("resume = %+v (%v)", resumed, err)
		}
		if got := <-again.frames; !strings.HasPrefix(got, "line one\r\nline two\r\n") {
			t.Errorf("replayed %q", got)
		}
		f.manager.Input(8, []byte("x"))
		if got := <-shell.input; got != "x" {
			t.Errorf("input after resume = %q", got)
		}
	})

	t.Run("ends a dropped session nobody resumes within 30 seconds", func(t *testing.T) {
		f := newFixture()
		opened, shell := f.open(t, 7, newFakeOutput())
		f.manager.StreamClosed(protocol.StreamClosePayload{StreamID: 7, KeepSession: true})
		if err := f.clock.WaitForTimers(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		f.clock.Advance(terminal.ResumeWindow)
		<-shell.closed
		_, err := f.manager.Open(protocol.TerminalOpenPayload{StreamID: 8, ResumeSessionID: opened.SessionID}, newFakeOutput())
		if !errors.Is(err, terminal.ErrSessionEnded) {
			t.Errorf("resume after expiry = %v", err)
		}
	})

	t.Run("can't resume a session that is still attached or never existed", func(t *testing.T) {
		f := newFixture()
		opened, _ := f.open(t, 7, newFakeOutput())
		for _, id := range []string{opened.SessionID, "unknown"} {
			if _, err := f.manager.Open(protocol.TerminalOpenPayload{StreamID: 9, ResumeSessionID: id}, newFakeOutput()); !errors.Is(err, terminal.ErrSessionEnded) {
				t.Errorf("%s: err = %v", id, err)
			}
		}
	})

	t.Run("keeps sessions when the agent's connection drops, and ends them all when it stops", func(t *testing.T) {
		f := newFixture()
		opened, shell := f.open(t, 7, newFakeOutput())
		f.manager.DetachAll()
		if _, err := f.manager.Open(protocol.TerminalOpenPayload{StreamID: 1, ResumeSessionID: opened.SessionID}, newFakeOutput()); err != nil {
			t.Errorf("resume after reconnect = %v", err)
		}
		f.manager.CloseAll()
		<-shell.closed
	})

	t.Run("treats a viewer that can't receive output as gone", func(t *testing.T) {
		f := newFixture()
		out := newFakeOutput()
		out.fail = true
		opened, shell := f.open(t, 7, out)
		shell.printAndSettle(t, "lost")
		again := newFakeOutput()
		if _, err := f.manager.Open(protocol.TerminalOpenPayload{StreamID: 8, ResumeSessionID: opened.SessionID}, again); err != nil {
			t.Fatalf("resume = %v", err)
		}
		if got := <-again.frames; !strings.HasPrefix(got, "lost") {
			t.Errorf("replayed %q", got)
		}
	})

	t.Run("refuses more than 32 sessions and reports a shell that won't start", func(t *testing.T) {
		f := newFixture()
		for i := range 32 {
			f.open(t, protocol.StreamID(i+1), newFakeOutput())
		}
		if _, err := f.manager.Open(protocol.TerminalOpenPayload{StreamID: 99}, newFakeOutput()); !errors.Is(err, terminal.ErrTooManySessions) {
			t.Errorf("33rd session: %v", err)
		}
		broken := terminal.NewManager(func(uint16, uint16) (terminal.Shell, error) { return nil, errors.New("no shell") },
			f.clock, slog.New(slog.DiscardHandler))
		if _, err := broken.Open(protocol.TerminalOpenPayload{StreamID: 1}, newFakeOutput()); err == nil {
			t.Error("Open returned nil")
		}
		if err := broken.Resize(protocol.TerminalResizePayload{StreamID: 1}); !errors.Is(err, terminal.ErrSessionEnded) {
			t.Errorf("resize unknown = %v", err)
		}
	})
}

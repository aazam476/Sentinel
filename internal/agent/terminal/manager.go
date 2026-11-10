// SPDX-License-Identifier: Apache-2.0

package terminal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// ResumeWindow is how long a shell keeps running after its viewer goes
// away, waiting to be reattached (§9.2).
const ResumeWindow = 30 * time.Second

// maxSessions caps the shells one machine runs for the web UI at once.
const maxSessions = 32

// Errors from Open. They are never reassigned.
var (
	// ErrSessionEnded: the session to resume has ended or never existed.
	ErrSessionEnded = errors.New("that terminal session has ended")
	// ErrTooManySessions: maxSessions shells are already running.
	ErrTooManySessions = errors.New("this machine already runs the most terminal sessions allowed (32)")
)

// Output is how a session sends to the server: the agent's connection.
type Output interface {
	SendFrame(ctx context.Context, stream protocol.StreamID, data []byte) error
	CloseStream(ctx context.Context, stream protocol.StreamID, reason string)
}

// Clock tells the time and makes timers.
type Clock interface {
	Now() time.Time
	NewTimer(duration time.Duration) clock.Timer
}

// Manager runs the terminal sessions of one agent.
type Manager struct {
	start  Starter
	clock  Clock
	logger *slog.Logger

	mutex    sync.Mutex
	sessions map[string]*session
	// byStream finds the session attached to a stream.
	byStream map[protocol.StreamID]*session
	// changed hears how many shells run whenever that changes.
	changed func(running int)
}

// OnChange calls changed with the number of running shells whenever a
// shell starts or ends, for the machine's session indicator (SEC-24).
func (m *Manager) OnChange(changed func(running int)) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.changed = changed
}

// notify tells OnChange's function how many shells run. It is called
// without the lock held.
func (m *Manager) notify() {
	m.mutex.Lock()
	changed, running := m.changed, len(m.sessions)
	m.mutex.Unlock()
	if changed != nil {
		changed(running)
	}
}

// NewManager returns a Manager that starts shells with start.
func NewManager(start Starter, clock Clock, logger *slog.Logger) *Manager {
	return &Manager{
		start: start, clock: clock, logger: logger,
		sessions: map[string]*session{}, byStream: map[protocol.StreamID]*session{},
	}
}

// Open starts a shell for the stream, or reattaches the stream to a
// session waiting to be resumed.
func (m *Manager) Open(request protocol.TerminalOpenPayload, out Output) (protocol.TerminalOpenedPayload, error) {
	return m.OpenWith(request, out, m.start)
}

// OpenWith is Open with another way to start the shell, such as a shell in
// a container. The session is then like any other: input, resizes, closes
// and resuming go through the same stream.
func (m *Manager) OpenWith(request protocol.TerminalOpenPayload, out Output, start Starter) (protocol.TerminalOpenedPayload, error) {
	if request.ResumeSessionID != "" {
		return m.resume(request, out)
	}
	m.mutex.Lock()
	full := len(m.sessions) >= maxSessions
	m.mutex.Unlock()
	if full {
		return protocol.TerminalOpenedPayload{}, ErrTooManySessions
	}
	shell, err := start(max(request.Cols, 1), max(request.Rows, 1))
	if err != nil {
		return protocol.TerminalOpenedPayload{}, fmt.Errorf("start a shell: %w", err)
	}
	s := &session{id: protocol.NewID(m.clock.Now()), shell: shell, manager: m}
	m.mutex.Lock()
	m.sessions[s.id] = s
	m.mutex.Unlock()
	m.notify()
	m.attach(s, request.StreamID, out)
	go s.copyOutput()
	m.logger.Info("terminal session started", "session", s.id)
	return protocol.TerminalOpenedPayload{SessionID: s.id}, nil
}

func (m *Manager) resume(request protocol.TerminalOpenPayload, out Output) (protocol.TerminalOpenedPayload, error) {
	m.mutex.Lock()
	s, ok := m.sessions[request.ResumeSessionID]
	m.mutex.Unlock()
	if !ok || !s.waitingForViewer() {
		return protocol.TerminalOpenedPayload{}, ErrSessionEnded
	}
	m.attach(s, request.StreamID, out)
	_ = s.shell.Resize(max(request.Cols, 1), max(request.Rows, 1)) // the old size still works if this fails
	m.logger.Info("terminal session resumed", "session", s.id)
	return protocol.TerminalOpenedPayload{SessionID: s.id, Resumed: true}, nil
}

func (m *Manager) attach(s *session, stream protocol.StreamID, out Output) {
	m.mutex.Lock()
	m.byStream[stream] = s
	m.mutex.Unlock()
	s.attach(stream, out)
}

func (m *Manager) sessionOf(stream protocol.StreamID) (*session, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	s, ok := m.byStream[stream]
	return s, ok
}

// Input sends the viewer's keystrokes to the shell behind the stream.
func (m *Manager) Input(stream protocol.StreamID, data []byte) {
	if s, ok := m.sessionOf(stream); ok {
		_, _ = s.shell.Write(data) // a shell that has exited ends its session on its own
	}
}

// Resize changes the size of the terminal behind the stream.
func (m *Manager) Resize(request protocol.TerminalResizePayload) error {
	s, ok := m.sessionOf(request.StreamID)
	if !ok {
		return ErrSessionEnded
	}
	return s.shell.Resize(max(request.Cols, 1), max(request.Rows, 1))
}

// StreamClosed handles the server closing a stream: the user closed the
// terminal, or the viewer went away and the session waits to be resumed.
func (m *Manager) StreamClosed(request protocol.StreamClosePayload) {
	s, ok := m.sessionOf(request.StreamID)
	if !ok {
		return
	}
	m.forgetStream(request.StreamID)
	if request.KeepSession {
		s.detach(request.StreamID)
		return
	}
	s.end("")
}

// DetachAll detaches every session, when the agent's connection drops: the
// streams are gone, but the shells wait to be resumed.
func (m *Manager) DetachAll() {
	m.mutex.Lock()
	attached := m.byStream
	m.byStream = map[protocol.StreamID]*session{}
	m.mutex.Unlock()
	for stream, s := range attached {
		s.detach(stream)
	}
}

// CloseAll ends every session, when the agent stops.
func (m *Manager) CloseAll() {
	m.mutex.Lock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mutex.Unlock()
	for _, s := range sessions {
		s.end("the agent is stopping")
	}
}

func (m *Manager) forgetStream(stream protocol.StreamID) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.byStream, stream)
}

// forget removes an ended session.
func (m *Manager) forget(s *session) {
	m.mutex.Lock()
	delete(m.sessions, s.id)
	for stream, attached := range m.byStream {
		if attached == s {
			delete(m.byStream, stream)
		}
	}
	m.mutex.Unlock()
	m.notify()
}

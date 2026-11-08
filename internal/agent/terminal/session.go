// SPDX-License-Identifier: Apache-2.0

package terminal

import (
	"context"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// sendTimeout is how long sending one piece of output may take before the
// viewer counts as gone.
const sendTimeout = 10 * time.Second

// session is one shell and whoever is watching it.
type session struct {
	id      string
	shell   Shell
	manager *Manager

	mutex   sync.Mutex
	history history
	// out and stream are the attached viewer; out is nil while detached.
	out    Output
	stream protocol.StreamID
	// expiry ends the session if no viewer reattaches in time.
	expiry clock.Timer
	ended  bool
}

// attach makes stream the session's viewer and sends it the recent output,
// so a reattaching viewer can redraw the screen.
func (s *session) attach(stream protocol.StreamID, out Output) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
	s.out, s.stream = out, stream
	if replay := s.history.bytes(); len(replay) > 0 {
		s.sendLocked(replay)
	}
}

// detach drops the viewer of stream and waits ResumeWindow for a new one.
func (s *session) detach(stream protocol.StreamID) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.ended || s.out == nil || s.stream != stream {
		return
	}
	s.detachLocked()
}

// detachLocked drops the viewer and starts the resume window; the caller
// holds s.mutex.
func (s *session) detachLocked() {
	s.out = nil
	timer := s.manager.clock.NewTimer(ResumeWindow)
	s.expiry = timer
	go func() {
		<-timer.C()
		s.mutex.Lock()
		expired := s.expiry == timer
		s.mutex.Unlock()
		if expired {
			s.end("")
		}
	}()
}

// waitingForViewer reports whether the session can be resumed.
func (s *session) waitingForViewer() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return !s.ended && s.out == nil
}

// copyOutput sends the shell's output to the viewer until the shell exits.
func (s *session) copyOutput() {
	buffer := make([]byte, 32<<10)
	for {
		n, err := s.shell.Read(buffer)
		if n > 0 {
			s.mutex.Lock()
			s.history.write(buffer[:n])
			if s.out != nil {
				s.sendLocked(append([]byte(nil), buffer[:n]...))
			}
			s.mutex.Unlock()
		}
		if err != nil {
			s.end("the shell exited")
			return
		}
	}
}

// sendLocked sends output to the viewer; the caller holds s.mutex. A viewer
// that can't take it is treated as gone, as if its stream had dropped.
func (s *session) sendLocked(data []byte) {
	ctx, cancel := clock.WithTimeout(context.Background(), s.manager.clock, sendTimeout)
	defer cancel()
	if err := s.out.SendFrame(ctx, s.stream, data); err != nil {
		s.detachLocked()
	}
}

// end stops the shell and forgets the session. reason, if not empty, is
// sent to the viewer as the stream's close reason.
func (s *session) end(reason string) {
	s.mutex.Lock()
	if s.ended {
		s.mutex.Unlock()
		return
	}
	s.ended = true
	out, stream := s.out, s.stream
	s.out = nil
	if s.expiry != nil {
		s.expiry.Stop()
	}
	s.mutex.Unlock()

	_ = s.shell.Close() // ending it anyway; an error means it was already gone
	s.manager.forget(s)
	if out != nil && reason != "" {
		out.CloseStream(context.Background(), stream, reason)
	}
	s.manager.logger.Info("terminal session ended", "session", s.id)
}

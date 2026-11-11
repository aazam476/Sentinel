// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"sync"
)

// outboxSize is how many messages may wait for a slow tray before it is
// dropped; a tray that reads nothing can't hold the agent up.
const outboxSize = 16

// ErrNoTray means no tray is connected: no one signed in at the machine
// runs one.
var ErrNoTray = errors.New("no one signed in at this machine is running the Sentinel tray")

// Control is the agent, as the tray's commands reach it.
type Control interface {
	// EndSession ends the remote session with this ID.
	EndSession(id string) error
	// SetRemoteAccessPaused refuses new terminal and desktop sessions and
	// ends the open ones, or allows them again.
	SetRemoteAccessPaused(paused bool)
}

// ServerOptions configure a Server.
type ServerOptions struct {
	// Listener accepts the trays' connections (Listen).
	Listener net.Listener
	// PauseFile records a pause, so a restart doesn't undo it; "" keeps
	// it in memory only.
	PauseFile string
	Logger    *slog.Logger
}

// Server is the agent's side of the tray socket. It tells every connected
// tray the agent's status, asks them to allow desktop sessions, and takes
// their commands.
type Server struct {
	options ServerOptions

	mutex   sync.Mutex
	status  Status
	clients map[*client]struct{}
	// asks maps each open question to where its answer goes.
	asks map[string]chan bool
}

// client is one connected tray.
type client struct {
	conn   net.Conn
	outbox chan Message
	once   sync.Once
}

func (c *client) close() {
	c.once.Do(func() { _ = c.conn.Close() }) // closing either way
}

// NewServer returns a Server; call Serve to accept trays.
func NewServer(options ServerOptions) *Server {
	return &Server{options: options, clients: map[*client]struct{}{}, asks: map[string]chan bool{}}
}

// Serve restores a recorded pause, then accepts trays until ctx ends.
func (s *Server) Serve(ctx context.Context, control Control) {
	if s.options.PauseFile != "" {
		if _, err := os.Stat(s.options.PauseFile); err == nil {
			control.SetRemoteAccessPaused(true)
		}
	}
	go func() {
		<-ctx.Done()
		_ = s.options.Listener.Close() // stops Accept below
	}()
	var clients sync.WaitGroup
	defer clients.Wait()
	for {
		conn, err := s.options.Listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				s.options.Logger.Warn("the tray socket stopped accepting connections", "error", err)
			}
			s.dropAll()
			return
		}
		clients.Go(func() { s.serveClient(ctx, conn, control) })
	}
}

// serveClient sends the tray the status, then handles its commands until
// it goes away.
func (s *Server) serveClient(ctx context.Context, conn net.Conn, control Control) {
	c := &client{conn: conn, outbox: make(chan Message, outboxSize)}
	defer c.close()
	s.mutex.Lock()
	status := s.status
	s.clients[c] = struct{}{}
	c.outbox <- Message{Type: TypeStatus, Status: &status}
	s.mutex.Unlock()
	defer s.drop(c)

	go c.writeAll()
	reader := NewReader(conn)
	for ctx.Err() == nil {
		message, err := reader.Read()
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				s.options.Logger.Info("dropped a tray connection", "error", err)
			}
			return
		}
		s.handle(message, control)
	}
}

// writeAll sends the client's messages until its outbox closes.
func (c *client) writeAll() {
	for message := range c.outbox {
		line, err := Encode(message)
		if err == nil {
			_, err = c.conn.Write(line)
		}
		if err != nil {
			c.close()
			for range c.outbox {
				// Drained, so a broadcast never blocks on a dead tray.
			}
			return
		}
	}
}

// handle carries out a tray's command.
func (s *Server) handle(message Message, control Control) {
	switch message.Type {
	case TypeAnswer:
		s.mutex.Lock()
		answer, open := s.asks[message.ID]
		s.mutex.Unlock()
		if open {
			select {
			case answer <- message.Allow:
			default: // someone else answered first
			}
		}
	case TypeEnd:
		if err := control.EndSession(message.ID); err != nil {
			s.options.Logger.Info("couldn't end a remote session from the tray", "session", message.ID, "error", err)
			return
		}
		s.options.Logger.Info("the person at the machine ended a remote session", "session", message.ID)
	case TypePause:
		s.setPaused(message.Paused, control)
	default:
		s.options.Logger.Info("ignored a tray message the agent doesn't take", "type", message.Type)
	}
}

// setPaused records and applies a pause or a resume.
func (s *Server) setPaused(paused bool, control Control) {
	if err := s.recordPause(paused); err != nil {
		s.options.Logger.Warn("couldn't record the pause; a restart will undo it", "error", err)
	}
	control.SetRemoteAccessPaused(paused)
	if paused {
		s.options.Logger.Info("the person at the machine paused remote access")
	} else {
		s.options.Logger.Info("the person at the machine resumed remote access")
	}
}

func (s *Server) recordPause(paused bool) error {
	path := s.options.PauseFile
	if path == "" {
		return nil
	}
	if paused {
		if err := os.WriteFile(path, []byte("paused from the tray\n"), 0o600); err != nil {
			return fmt.Errorf("record the pause: %w", err)
		}
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clear the pause: %w", err)
	}
	return nil
}

// Publish sends every tray the agent's new status.
func (s *Server) Publish(status Status) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.status = status
	s.broadcast(Message{Type: TypeStatus, Status: &status})
}

// broadcast queues a message for every client, dropping any that can't
// keep up. It holds the lock.
func (s *Server) broadcast(message Message) {
	for c := range s.clients {
		select {
		case c.outbox <- message:
		default:
			s.options.Logger.Info("dropped a tray that stopped reading")
			s.remove(c)
		}
	}
}

// Connected reports whether any tray is connected.
func (s *Server) Connected() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return len(s.clients) > 0
}

// Ask puts the question to every connected tray and returns the first
// answer (SEC-23). It returns ErrNoTray when there is no one to ask.
func (s *Server) Ask(ctx context.Context, question string) (bool, error) {
	id := rand.Text()
	answer := make(chan bool, 1)
	s.mutex.Lock()
	if len(s.clients) == 0 {
		s.mutex.Unlock()
		return false, ErrNoTray
	}
	s.asks[id] = answer
	s.broadcast(Message{Type: TypeAsk, ID: id, Question: question})
	s.mutex.Unlock()

	defer func() {
		s.mutex.Lock()
		delete(s.asks, id)
		s.broadcast(Message{Type: TypeAsked, ID: id})
		s.mutex.Unlock()
	}()
	select {
	case allowed := <-answer:
		return allowed, nil
	case <-ctx.Done():
		return false, fmt.Errorf("ask the person at the machine: %w", context.Cause(ctx))
	}
}

// Show is the session indicator (SEC-24): the trays show the open
// sessions from the status, so it only checks that one is there to.
func (s *Server) Show(context.Context, string) (io.Closer, error) {
	if !s.Connected() {
		return nil, ErrNoTray
	}
	return nopCloser{}, nil
}

func (s *Server) drop(c *client) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.remove(c)
}

// remove forgets a client and closes its outbox. It holds the lock.
func (s *Server) remove(c *client) {
	if _, ok := s.clients[c]; !ok {
		return
	}
	delete(s.clients, c)
	close(c.outbox)
	c.close()
}

func (s *Server) dropAll() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for c := range s.clients {
		s.remove(c)
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/tray"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// trayClient is a tray connected to the agent's socket.
type trayClient struct {
	conn   net.Conn
	reader *tray.Reader
}

// withTray gives the next agent a tray socket and connects a tray to it.
func (s *server) withTray(t *testing.T) func() *trayClient {
	t.Helper()
	dir, err := os.MkdirTemp("", "core-tray") // short: socket paths are limited to about 100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "tray.sock")
	listener, err := tray.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	s.machine.Tray = tray.NewServer(tray.ServerOptions{Listener: listener, Logger: slog.New(slog.DiscardHandler)})
	return func() *trayClient {
		conn, err := tray.Dial(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return &trayClient{conn: conn, reader: tray.NewReader(conn)}
	}
}

// next reads the next message.
func (c *trayClient) next(t *testing.T) tray.Message {
	t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	message, err := c.reader.Read()
	if err != nil {
		t.Fatalf("tray read: %v", err)
	}
	return message
}

// statusWhere reads until a status matches.
func (c *trayClient) statusWhere(t *testing.T, matches func(tray.Status) bool) tray.Status {
	t.Helper()
	for {
		if message := c.next(t); message.Type == tray.TypeStatus && matches(*message.Status) {
			return *message.Status
		}
	}
}

func (c *trayClient) send(t *testing.T, message tray.Message) {
	t.Helper()
	line, err := tray.Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.conn.Write(line); err != nil {
		t.Fatal(err)
	}
}

func waitForEnd(t *testing.T, stream *hub.Stream) string {
	t.Helper()
	select {
	case <-stream.Done():
		return stream.Reason()
	case <-time.After(10 * time.Second):
		t.Fatal("the stream didn't end")
		return ""
	}
}

func TestSEC24_TheTrayListsSessionsAndEndsThem(t *testing.T) {
	s := newServer(t)
	s.withEchoShells()
	connect := s.withTray(t)
	agent := desktopAgent(t, s, "show_session_indicator = true\n")
	c := connect()

	status := c.statusWhere(t, func(status tray.Status) bool { return status.Connected })
	if status.Server != agent.state.ServerURL || status.Version != "v0.1.1" || status.ConsentRequired {
		t.Errorf("status = %+v", status)
	}
	terminal, _, err := s.openTerminal(t, agent.state.AgentID, "")
	if err != nil {
		t.Fatal(err)
	}
	desktop, _, err := s.openDesktop(t, agent.state.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	status = c.statusWhere(t, func(status tray.Status) bool { return len(status.Sessions) == 2 })
	for _, session := range status.Sessions {
		if session.User != "admin" || session.ID == "" {
			t.Errorf("session = %+v, want admin's", session)
		}
	}

	for _, session := range status.Sessions {
		c.send(t, tray.Message{Type: tray.TypeEnd, ID: session.ID})
	}
	for _, stream := range []*hub.Stream{terminal, desktop} {
		if reason := waitForEnd(t, stream); reason != "The person at the machine ended the session." {
			t.Errorf("reason = %q", reason)
		}
	}
	c.statusWhere(t, func(status tray.Status) bool { return len(status.Sessions) == 0 })
}

func TestTheTrayPausesRemoteAccess(t *testing.T) {
	s := newServer(t)
	s.withEchoShells()
	connect := s.withTray(t)
	agent := desktopAgent(t, s, "")
	c := connect()
	c.statusWhere(t, func(status tray.Status) bool { return status.Connected })

	open, _, err := s.openTerminal(t, agent.state.AgentID, "")
	if err != nil {
		t.Fatal(err)
	}
	c.send(t, tray.Message{Type: tray.TypePause, Paused: true})
	if reason := waitForEnd(t, open); reason != "The person at the machine paused remote access." {
		t.Errorf("an open terminal ended with %q", reason)
	}
	if status := c.statusWhere(t, func(status tray.Status) bool { return status.Paused }); len(status.Sessions) != 0 {
		t.Errorf("sessions listed without an indicator policy: %+v", status.Sessions)
	}

	for name, attempt := range map[string]func() error{
		"terminal": func() error { _, _, err := s.openTerminal(t, agent.state.AgentID, ""); return err },
		"desktop":  func() error { _, _, err := s.openDesktop(t, agent.state.AgentID); return err },
	} {
		var agentErr *protocol.Error
		if err := attempt(); !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied || !strings.Contains(agentErr.Message, "paused") {
			t.Errorf("a %s while paused: %v, want a refusal", name, err)
		}
	}

	c.send(t, tray.Message{Type: tray.TypePause, Paused: false})
	c.statusWhere(t, func(status tray.Status) bool { return !status.Paused })
	if _, _, err := s.openTerminal(t, agent.state.AgentID, ""); err != nil {
		t.Errorf("a terminal after resuming: %v", err)
	}
}

func TestSEC23_TheTrayAsksBeforeRemoteDesktop(t *testing.T) {
	s := newServer(t)
	connect := s.withTray(t)
	agent := desktopAgent(t, s, "desktop_requires_consent = true\n")

	t.Run("refuses with no tray to ask", func(t *testing.T) {
		var agentErr *protocol.Error
		if _, _, err := s.openDesktop(t, agent.state.AgentID); !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied {
			t.Errorf("err = %v, want a refusal", err)
		}
	})

	t.Run("starts once the tray allows it", func(t *testing.T) {
		c := connect()
		c.statusWhere(t, func(status tray.Status) bool { return status.ConsentRequired })
		go func() {
			for {
				message := c.next(t)
				if message.Type == tray.TypeAsk {
					c.send(t, tray.Message{Type: tray.TypeAnswer, ID: message.ID, Allow: true})
					return
				}
			}
		}()
		stream, _, err := s.openDesktop(t, agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		stream.Close(t.Context(), "closed by the user")
	})
}

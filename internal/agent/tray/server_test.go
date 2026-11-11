// SPDX-License-Identifier: Apache-2.0

package tray_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

// fakeControl records the commands the trays send.
type fakeControl struct {
	mutex  sync.Mutex
	ended  []string
	paused []bool
	// changed hears every command.
	changed chan struct{}
	endErr  error
}

func newControl() *fakeControl {
	return &fakeControl{changed: make(chan struct{}, 16)}
}

func (c *fakeControl) EndSession(id string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.ended = append(c.ended, id)
	c.changed <- struct{}{}
	return c.endErr
}

func (c *fakeControl) SetRemoteAccessPaused(paused bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.paused = append(c.paused, paused)
	c.changed <- struct{}{}
}

func (c *fakeControl) wait(t *testing.T) {
	t.Helper()
	select {
	case <-c.changed:
	case <-t.Context().Done():
		t.Fatal("no command arrived")
	}
}

// serverHarness runs a Server on a real socket in a temporary folder.
type serverHarness struct {
	server    *tray.Server
	control   *fakeControl
	path      string
	pauseFile string
}

func newServer(t *testing.T) *serverHarness {
	t.Helper()
	dir, err := os.MkdirTemp("", "tray") // short: socket paths are limited to about 100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	h := &serverHarness{control: newControl(), path: filepath.Join(dir, "sock", "tray.sock"), pauseFile: filepath.Join(dir, "paused")}
	h.start(t)
	return h
}

func (h *serverHarness) start(t *testing.T) {
	t.Helper()
	listener, err := tray.Listen(h.path)
	if err != nil {
		t.Fatal(err)
	}
	h.server = tray.NewServer(tray.ServerOptions{Listener: listener, PauseFile: h.pauseFile, Logger: slog.New(slog.DiscardHandler)})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { h.server.Serve(ctx, h.control); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

// trayConn is a raw tray connection.
type trayConn struct {
	conn   net.Conn
	reader *tray.Reader
}

func (h *serverHarness) connect(t *testing.T) *trayConn {
	t.Helper()
	conn, err := tray.Dial(t.Context(), h.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := &trayConn{conn: conn, reader: tray.NewReader(conn)}
	if first := c.read(t); first.Type != tray.TypeStatus {
		t.Fatalf("first message = %+v, want the status", first)
	}
	waitFor(t, h.server.Connected)
	return c
}

func (c *trayConn) read(t *testing.T) tray.Message {
	t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	message, err := c.reader.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return message
}

func (c *trayConn) send(t *testing.T, message tray.Message) {
	t.Helper()
	line, err := tray.Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.conn.Write(line); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls a condition that another goroutine makes true soon.
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	for !condition() {
		select {
		case <-t.Context().Done():
			t.Fatal("condition never became true")
		default:
			runtime.Gosched()
		}
	}
}

func TestTraysHearTheStatusOnConnectAndOnEveryChange(t *testing.T) {
	h := newServer(t)
	h.server.Publish(*sampleStatus())
	first := h.connect(t)

	status := *sampleStatus()
	status.Paused = true
	h.server.Publish(status)

	if got := first.read(t); got.Type != tray.TypeStatus || !got.Status.Paused {
		t.Errorf("after Publish = %+v, want the paused status", got)
	}
	second, err := tray.Dial(t.Context(), h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	got, err := tray.NewReader(second).Read()
	if err != nil || !got.Status.Paused || len(got.Status.Sessions) != 1 {
		t.Errorf("a tray connecting later got %+v, %v; want the current status", got, err)
	}
}

func TestSEC23_TheFirstTrayToAnswerDecides(t *testing.T) {
	h := newServer(t)
	if _, err := h.server.Ask(t.Context(), "Allow it?"); !errors.Is(err, tray.ErrNoTray) {
		t.Fatalf("Ask with no tray = %v, want ErrNoTray", err)
	}
	first, second := h.connect(t), h.connect(t)

	answered := make(chan bool, 1)
	go func() {
		allowed, err := h.server.Ask(t.Context(), "admin wants to see this screen through Sentinel. Allow it?")
		if err != nil {
			t.Error(err)
		}
		answered <- allowed
	}()
	ask := first.read(t)
	if again := second.read(t); ask.Type != tray.TypeAsk || again.ID != ask.ID {
		t.Fatalf("asks = %+v and %+v, want the same question on both trays", ask, again)
	}
	second.send(t, tray.Message{Type: tray.TypeAnswer, ID: ask.ID, Allow: false})
	if allowed := <-answered; allowed {
		t.Error("Ask = true, want the second tray's no")
	}
	if done := first.read(t); done.Type != tray.TypeAsked || done.ID != ask.ID {
		t.Errorf("the other tray got %+v, want the question taken away", done)
	}
	first.send(t, tray.Message{Type: tray.TypeAnswer, ID: ask.ID, Allow: true}) // too late: ignored
}

func TestSEC23_AnUnansweredQuestionGivesUp(t *testing.T) {
	h := newServer(t)
	c := h.connect(t)
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := h.server.Ask(ctx, "Allow it?")
		result <- err
	}()
	ask := c.read(t)
	cancel()
	if err := <-result; err == nil {
		t.Error("Ask = nil, want the context's error")
	}
	if done := c.read(t); done.Type != tray.TypeAsked || done.ID != ask.ID {
		t.Errorf("got %+v, want the question taken away", done)
	}
}

func TestTrayCommandsReachTheAgent(t *testing.T) {
	h := newServer(t)
	c := h.connect(t)

	t.Run("ends a session", func(t *testing.T) {
		c.send(t, tray.Message{Type: tray.TypeEnd, ID: "s1"})
		h.control.wait(t)
		h.control.mutex.Lock()
		h.control.endErr = errors.New("no such session")
		h.control.mutex.Unlock()
		c.send(t, tray.Message{Type: tray.TypeEnd, ID: "gone"})
		h.control.wait(t)
		if len(h.control.ended) != 2 || h.control.ended[0] != "s1" {
			t.Errorf("ended = %v", h.control.ended)
		}
	})

	t.Run("pauses, and remembers it", func(t *testing.T) {
		c.send(t, tray.Message{Type: tray.TypePause, Paused: true})
		h.control.wait(t)
		if _, err := os.Stat(h.pauseFile); err != nil {
			t.Errorf("pause file: %v", err)
		}
	})

	t.Run("a restarted agent stays paused", func(t *testing.T) {
		restarted := &serverHarness{control: newControl(), path: h.path + "2", pauseFile: h.pauseFile}
		restarted.start(t)
		restarted.control.wait(t)
		if got := restarted.control.paused; len(got) != 1 || !got[0] {
			t.Errorf("paused = %v, want [true]", got)
		}
	})

	t.Run("resumes", func(t *testing.T) {
		c.send(t, tray.Message{Type: tray.TypePause, Paused: false})
		h.control.wait(t)
		if _, err := os.Stat(h.pauseFile); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("pause file after resuming: %v", err)
		}
		if got := h.control.paused; len(got) != 2 || got[1] {
			t.Errorf("paused = %v, want [true false]", got)
		}
	})

	t.Run("ignores the agent's own messages and drops a tray that sends nonsense", func(t *testing.T) {
		c.send(t, tray.Message{Type: tray.TypeAsked, ID: "x"})
		if _, err := c.conn.Write([]byte("nonsense\n")); err != nil {
			t.Fatal(err)
		}
		if err := c.conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := c.reader.Read(); !errors.Is(err, io.EOF) {
			t.Errorf("read after nonsense = %v, want the connection closed", err)
		}
		waitFor(t, func() bool { return !h.server.Connected() })
	})
}

func TestATrayThatStopsReadingIsDropped(t *testing.T) {
	h := newServer(t)
	h.connect(t) // never reads again
	status := *sampleStatus()
	status.Server = strings.Repeat("a", 8000) // fills the socket's buffer sooner
	for range 10000 {
		h.server.Publish(status)
		if !h.server.Connected() {
			return
		}
	}
	t.Error("a tray that reads nothing is still connected after 10,000 updates")
}

func TestSEC24_ShowNeedsATray(t *testing.T) {
	h := newServer(t)
	if _, err := h.server.Show(t.Context(), "Remote session"); !errors.Is(err, tray.ErrNoTray) {
		t.Errorf("Show with no tray = %v, want ErrNoTray", err)
	}
	h.connect(t)
	shown, err := h.server.Show(t.Context(), "Remote session")
	if err != nil || shown.Close() != nil {
		t.Errorf("Show with a tray = %v", err)
	}
}

// fakeNotifier is the machine's own way to ask.
type fakeNotifier struct{ asked, shown int }

func (n *fakeNotifier) Ask(context.Context, string) (bool, error) {
	n.asked++
	return true, nil
}

func (n *fakeNotifier) Show(context.Context, string) (io.Closer, error) {
	n.shown++
	return io.NopCloser(nil), nil
}

func TestWithFallbackUsesTheMachinesOwnWayWithoutATray(t *testing.T) {
	h := newServer(t)
	machine := &fakeNotifier{}
	notifier := tray.WithFallback(h.server, machine)
	if allowed, err := notifier.Ask(t.Context(), "Allow it?"); err != nil || !allowed || machine.asked != 1 {
		t.Errorf("Ask = %v, %v (asked %d), want the machine's answer", allowed, err, machine.asked)
	}
	if _, err := notifier.Show(t.Context(), "Remote session"); err != nil || machine.shown != 1 {
		t.Errorf("Show = %v (shown %d)", err, machine.shown)
	}

	h.connect(t)
	if _, err := notifier.Show(t.Context(), "Remote session"); err != nil || machine.shown != 1 {
		t.Errorf("with a tray, Show = %v and the machine showed %d, want the tray", err, machine.shown)
	}
	if _, err := tray.WithFallback(newServer(t).server, nil).Ask(t.Context(), "Allow it?"); !errors.Is(err, tray.ErrNoTray) {
		t.Errorf("no tray and no fallback: %v, want ErrNoTray", err)
	}
}

func TestListenOnlyServesPermittedUsers(t *testing.T) {
	h := newServer(t)
	info, err := os.Stat(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o666 {
		t.Errorf("socket mode = %v, want anyone to connect (peers are checked one by one)", info.Mode().Perm())
	}
	// This test runs as the agent's own user, who is always served.
	h.connect(t)
}

func TestAPauseThatCantBeRecordedStillApplies(t *testing.T) {
	dir, err := os.MkdirTemp("", "tray")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	h := &serverHarness{control: newControl(), path: filepath.Join(dir, "tray.sock"), pauseFile: filepath.Join(dir, "missing", "paused")}
	h.start(t)
	c := h.connect(t)
	c.send(t, tray.Message{Type: tray.TypePause, Paused: true})
	h.control.wait(t)
	if got := h.control.paused; len(got) != 1 || !got[0] {
		t.Errorf("paused = %v", got)
	}
}

func TestServeStopsWhenItsSocketCloses(t *testing.T) {
	dir, err := os.MkdirTemp("", "tray")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	listener, err := tray.Listen(filepath.Join(dir, "tray.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server := tray.NewServer(tray.ServerOptions{Listener: listener, Logger: slog.New(slog.DiscardHandler)})
	done := make(chan struct{})
	go func() { server.Serve(t.Context(), newControl()); close(done) }()
	_ = listener.Close()
	<-done
}

// SPDX-License-Identifier: Apache-2.0

package relay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/server/relay"
)

// fakeStream is the agent's end: a test feeds it output and reads what the
// browser typed.
type fakeStream struct {
	frames  chan []byte
	done    chan struct{}
	written chan string
	once    sync.Once

	mutex    sync.Mutex
	reason   string
	detached bool
}

func newFakeStream() *fakeStream {
	return &fakeStream{frames: make(chan []byte, 16), done: make(chan struct{}), written: make(chan string, 16)}
}

func (s *fakeStream) Frames() <-chan []byte { return s.frames }
func (s *fakeStream) Done() <-chan struct{} { return s.done }

func (s *fakeStream) Reason() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.reason
}

func (s *fakeStream) Write(_ context.Context, data []byte) error {
	s.written <- string(data)
	return nil
}

func (s *fakeStream) end(reason string, detached bool) {
	s.once.Do(func() {
		s.mutex.Lock()
		s.reason, s.detached = reason, detached
		s.mutex.Unlock()
		close(s.done)
	})
}

func (s *fakeStream) Close(_ context.Context, reason string) { s.end(reason, false) }
func (s *fakeStream) Detach(context.Context)                 { s.end("detached", true) }

// pipe runs relay.Pipe on the server side of a real WebSocket and returns
// the browser side, and the pipe's result once it ends.
func pipe(t *testing.T, stream relay.AgentStream, resize relay.Resize) (*websocket.Conn, <-chan relay.Result) {
	t.Helper()
	results := make(chan relay.Result, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		results <- relay.Pipe(r.Context(), conn, stream, relay.Status{SessionID: "s1"}, resize)
		_ = conn.Close(websocket.StatusNormalClosure, "") // as the API handler does
	}))
	t.Cleanup(server.Close)
	browser, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), nil) //nolint:bodyclose // closed by coder/websocket
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = browser.CloseNow() })
	return browser, results
}

func readStatus(t *testing.T, browser *websocket.Conn) relay.Status {
	t.Helper()
	kind, data, err := browser.Read(t.Context())
	if err != nil || kind != websocket.MessageText {
		t.Fatalf("read status: %v %v", kind, err)
	}
	var status relay.Status
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func readOutput(t *testing.T, browser *websocket.Conn) string {
	t.Helper()
	kind, data, err := browser.Read(t.Context())
	if err != nil || kind != websocket.MessageBinary {
		t.Fatalf("read output: %v %v", kind, err)
	}
	return string(data)
}

func sendControl(t *testing.T, browser *websocket.Conn, control string) {
	t.Helper()
	if err := browser.Write(t.Context(), websocket.MessageText, []byte(control)); err != nil {
		t.Fatal(err)
	}
}

func noResize(context.Context, uint16, uint16) error { return nil }

// readToEnd reads until the connection closes, answering the server's
// close as a browser does at once.
func readToEnd(browser *websocket.Conn) {
	for {
		if _, _, err := browser.Read(context.Background()); err != nil {
			return
		}
	}
}

func TestPipe(t *testing.T) {
	t.Run("says the terminal opened, then carries output and input", func(t *testing.T) {
		stream := newFakeStream()
		browser, results := pipe(t, stream, noResize)
		if status := readStatus(t, browser); status.Type != "opened" || status.SessionID != "s1" {
			t.Errorf("status = %+v", status)
		}
		stream.frames <- []byte("$ ")
		if got := readOutput(t, browser); got != "$ " {
			t.Errorf("output = %q", got)
		}
		if err := browser.Write(t.Context(), websocket.MessageBinary, []byte("ls\r")); err != nil {
			t.Fatal(err)
		}
		if got := <-stream.written; got != "ls\r" {
			t.Errorf("input = %q", got)
		}
		sendControl(t, browser, `{"type":"close"}`)
		readToEnd(browser)
		result := <-results
		if result.Reason != "closed by the user" || result.Detached || result.BytesIn != 3 || result.BytesOut != 2 {
			t.Errorf("result = %+v", result)
		}
	})

	t.Run("passes resizes on and ignores control messages it doesn't know", func(t *testing.T) {
		stream := newFakeStream()
		sizes := make(chan [2]uint16, 1)
		browser, _ := pipe(t, stream, func(_ context.Context, cols, rows uint16) error {
			sizes <- [2]uint16{cols, rows}
			return nil
		})
		readStatus(t, browser)
		sendControl(t, browser, `not json`)
		sendControl(t, browser, `{"type":"dance"}`)
		sendControl(t, browser, `{"type":"resize","cols":132,"rows":43}`)
		if got := <-sizes; got != [2]uint16{132, 43} {
			t.Errorf("size = %v", got)
		}
	})

	t.Run("detaches when the browser goes away, so the session can be resumed", func(t *testing.T) {
		stream := newFakeStream()
		browser, results := pipe(t, stream, noResize)
		readStatus(t, browser)
		_ = browser.CloseNow()
		result := <-results
		if !result.Detached || !stream.detached {
			t.Errorf("result = %+v, stream detached = %v", result, stream.detached)
		}
	})

	t.Run("sends the last output and the reason when the agent ends the stream", func(t *testing.T) {
		stream := newFakeStream()
		browser, results := pipe(t, stream, noResize)
		readStatus(t, browser)
		stream.frames <- []byte("logout\r\n")
		stream.end("the shell exited", false)
		if got := readOutput(t, browser); got != "logout\r\n" {
			t.Errorf("last output = %q", got)
		}
		if status := readStatus(t, browser); status.Type != "closed" || status.Reason != "the shell exited" {
			t.Errorf("status = %+v", status)
		}
		readToEnd(browser)
		if result := <-results; result.Detached || result.Reason != "the shell exited" {
			t.Errorf("result = %+v", result)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package relay_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/relay"
)

// pipeDesktop runs relay.PipeDesktop on the server side of a real
// WebSocket and returns the browser side, and the result once it ends.
func pipeDesktop(t *testing.T, stream relay.AgentStream) (*websocket.Conn, <-chan relay.Result) {
	t.Helper()
	results := make(chan relay.Result, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		results <- relay.PipeDesktop(r.Context(), conn, stream)
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

func TestPipeDesktop(t *testing.T) {
	t.Run("passes control messages both ways and the screen to the browser", func(t *testing.T) {
		stream := newFakeStream()
		browser, results := pipeDesktop(t, stream)
		if err := browser.Write(t.Context(), websocket.MessageText, []byte(`{"type":"ack"}`)); err != nil {
			t.Fatal(err)
		}
		if got := <-stream.written; got != `c{"type":"ack"}` {
			t.Errorf("agent got %q", got)
		}
		stream.frames <- []byte(`c{"type":"answer"}`)
		stream.frames <- []byte{protocol.DesktopFrame, protocol.FrameEnd}
		if kind, data, err := browser.Read(t.Context()); err != nil || kind != websocket.MessageText || string(data) != `{"type":"answer"}` {
			t.Errorf("browser got %v %q (%v)", kind, data, err)
		}
		if kind, data, err := browser.Read(t.Context()); err != nil || kind != websocket.MessageBinary || len(data) != 1 {
			t.Errorf("browser got %v %v (%v)", kind, data, err)
		}
		if err := browser.Close(websocket.StatusNormalClosure, ""); err != nil {
			t.Fatal(err)
		}
		result := <-results
		if result.Reason != "the viewer closed the session" || result.BytesIn != 14 || result.BytesOut != 18 {
			t.Errorf("result = %+v", result)
		}
		if stream.Reason() != "the viewer left" {
			t.Errorf("stream closed with %q", stream.Reason())
		}
	})

	t.Run("closes the browser with the agent's reason", func(t *testing.T) {
		stream := newFakeStream()
		browser, results := pipeDesktop(t, stream)
		stream.end("the screen locked", false)
		_, _, err := browser.Read(t.Context())
		if websocket.CloseStatus(err) != websocket.StatusNormalClosure || !strings.Contains(err.Error(), "The screen locked.") {
			t.Errorf("browser got %v", err)
		}
		if result := <-results; result.Reason != "the screen locked" {
			t.Errorf("result = %+v", result)
		}
	})

	t.Run("takes a browser that goes away as the end", func(t *testing.T) {
		stream := newFakeStream()
		browser, results := pipeDesktop(t, stream)
		_ = browser.CloseNow()
		if result := <-results; result.Reason != "the viewer went away" {
			t.Errorf("result = %+v", result)
		}
	})
}

func TestCloseReason(t *testing.T) {
	long := strings.Repeat("é", 100)
	tests := []struct {
		name, reason, want string
	}{
		{"makes a sentence of the agent's reason", "the screen locked", "The screen locked."},
		{"keeps a sentence as it is", "Nobody is signed in.", "Nobody is signed in."},
		{"gives nothing for no reason", "  ", ""},
		{"cuts a long reason at a whole character", long, "É" + strings.Repeat("é", 60)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := relay.CloseReason(test.reason)
			if got != test.want || len(got) > 123 || !utf8.ValidString(got) {
				t.Errorf("CloseReason(%q) = %q, want %q", test.reason, got, test.want)
			}
		})
	}
}

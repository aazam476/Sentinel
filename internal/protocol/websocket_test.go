// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
)

// pipe returns the two ends of a real WebSocket connection.
func pipe(t *testing.T) (client, server *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	done := make(chan struct{})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		accepted <- conn
		<-done // keep the handler, and so the connection, alive
	}))
	t.Cleanup(func() { close(done); httpServer.Close() })
	client, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(httpServer.URL, "http"), nil) //nolint:bodyclose // closed by coder/websocket
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.CloseNow() }) // may already be closed
	return client, <-accepted
}

func TestWebSocketMessages(t *testing.T) {
	t.Run("a message written on one end is read on the other", func(t *testing.T) {
		client, server := pipe(t)
		hello := protocol.HelloPayload{AgentID: "01JBX3K5Q7Z8M4N6P8R0T2V4W6", AgentVersion: "v1", ProtocolVersion: 1, AgentNonce: protocol.NewNonce()}
		if err := protocol.WriteMessage(t.Context(), client, protocol.Message{Type: protocol.TypeHello}, hello); err != nil {
			t.Fatal(err)
		}
		var got protocol.HelloPayload
		if err := protocol.ReadPayload(t.Context(), server, protocol.TypeHello, &got); err != nil {
			t.Fatal(err)
		}
		if got.AgentID != hello.AgentID {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("a message without a payload is sent as is", func(t *testing.T) {
		client, server := pipe(t)
		if err := protocol.WriteMessage(t.Context(), client, protocol.Message{Type: protocol.TypePing}, nil); err != nil {
			t.Fatal(err)
		}
		message, err := protocol.ReadMessage(t.Context(), server)
		if err != nil || message.Type != protocol.TypePing || message.Payload != nil {
			t.Errorf("got %+v (%v)", message, err)
		}
	})

	t.Run("ReadPayload refuses a message of another type", func(t *testing.T) {
		client, server := pipe(t)
		if err := protocol.WriteMessage(t.Context(), client, protocol.Message{Type: protocol.TypePing}, nil); err != nil {
			t.Fatal(err)
		}
		if err := protocol.ReadPayload(t.Context(), server, protocol.TypeProve, &protocol.ProvePayload{}); err == nil {
			t.Error("ReadPayload accepted a ping as prove")
		}
	})

	t.Run("a binary frame is reported as such", func(t *testing.T) {
		client, server := pipe(t)
		if err := client.Write(t.Context(), websocket.MessageBinary, protocol.EncodeFrame(7, []byte("x"))); err != nil {
			t.Fatal(err)
		}
		if _, err := protocol.ReadMessage(t.Context(), server); !errors.Is(err, protocol.ErrBinaryFrame) {
			t.Errorf("err = %v, want ErrBinaryFrame", err)
		}
	})

	t.Run("an invalid message is refused before sending", func(t *testing.T) {
		client, _ := pipe(t)
		if err := protocol.WriteMessage(t.Context(), client, protocol.Message{Type: "nonsense"}, nil); err == nil {
			t.Error("WriteMessage sent an invalid message")
		}
		if err := protocol.WriteMessage(t.Context(), client, protocol.Message{Type: protocol.TypePing}, func() {}); err == nil {
			t.Error("WriteMessage sent a payload that can't be encoded")
		}
	})

	t.Run("errors from a closed connection are returned", func(t *testing.T) {
		client, server := pipe(t)
		_ = client.CloseNow()
		if err := protocol.ReadPayload(t.Context(), server, protocol.TypeHello, &protocol.HelloPayload{}); err == nil {
			t.Error("read from a closed connection returned nil")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := protocol.WriteMessage(ctx, client, protocol.Message{Type: protocol.TypePing}, nil); err == nil {
			t.Error("write on a closed connection returned nil")
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package hub_test

import (
	"context"
	"errors"
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// openTerminal opens a stream to a connected test agent, which accepts it.
// It returns the server's end and the stream ID the agent was given.
func openTerminal(t *testing.T, h *harness, agent *testAgent) *hub.Stream {
	t.Helper()
	type opened struct {
		stream *hub.Stream
		err    error
	}
	result := make(chan opened, 1)
	go func() {
		var answer protocol.TerminalOpenedPayload
		stream, err := h.hub.OpenStream(t.Context(), h.agentID(t, agent), protocol.ActionTerminalOpen,
			func(id protocol.StreamID) any { return protocol.TerminalOpenPayload{StreamID: id, Cols: 80, Rows: 24} }, &answer)
		result <- opened{stream, err}
	}()
	request := agent.read()
	var payload protocol.TerminalOpenPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		t.Fatal(err)
	}
	// The agent sends output before it even answers: nothing may be lost.
	agent.sendFrame(payload.StreamID, "welcome\r\n")
	agent.send(protocol.Message{Type: protocol.TypeResponse, ID: request.ID}, protocol.TerminalOpenedPayload{SessionID: "s1"})
	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.stream.ID != payload.StreamID {
		t.Fatalf("stream %d, agent was told %d", got.stream.ID, payload.StreamID)
	}
	return got.stream
}

func (a *testAgent) sendFrame(stream protocol.StreamID, text string) {
	a.t.Helper()
	if err := protocol.WriteFrame(a.t.Context(), a.conn, stream, []byte(text)); err != nil {
		a.t.Fatal(err)
	}
}

// readFrame reads until the next binary frame.
func (a *testAgent) readFrame() (protocol.StreamID, string) {
	a.t.Helper()
	for {
		message, frame, err := protocol.ReadAny(a.t.Context(), a.conn)
		if err != nil {
			a.t.Fatal(err)
		}
		if frame == nil {
			if message.Type == protocol.TypeRequest {
				continue // e.g. system.info, not what this test is about
			}
			a.t.Fatalf("got %s, want a frame", message.Type)
		}
		id, data, err := protocol.DecodeFrame(frame)
		if err != nil {
			a.t.Fatal(err)
		}
		return id, string(data)
	}
}

func connectedAgent(t *testing.T, h *harness) *testAgent {
	t.Helper()
	agent := h.online(t)
	agent.answerSystemInfo(protocol.SystemInfo{Hostname: "nas-01"})
	return agent
}

func TestStreams(t *testing.T) {
	t.Run("carries data both ways, including what the agent sent before answering", func(t *testing.T) {
		h := newHarness(t)
		agent := connectedAgent(t, h)
		stream := openTerminal(t, h, agent)

		if got := string(<-stream.Frames()); got != "welcome\r\n" {
			t.Errorf("first frame = %q", got)
		}
		agent.sendFrame(stream.ID, "$ ")
		if got := string(<-stream.Frames()); got != "$ " {
			t.Errorf("second frame = %q", got)
		}
		if err := stream.Write(t.Context(), []byte("uptime\r")); err != nil {
			t.Fatal(err)
		}
		if id, data := agent.readFrame(); id != stream.ID || data != "uptime\r" {
			t.Errorf("agent got %d %q", id, data)
		}
	})

	t.Run("gives each stream its own ID", func(t *testing.T) {
		h := newHarness(t)
		agent := connectedAgent(t, h)
		first, second := openTerminal(t, h, agent), openTerminal(t, h, agent)
		if first.ID == second.ID {
			t.Errorf("both streams got ID %d", first.ID)
		}
	})

	t.Run("ends when the agent closes it, with the agent's reason", func(t *testing.T) {
		h := newHarness(t)
		agent := connectedAgent(t, h)
		stream := openTerminal(t, h, agent)
		agent.send(protocol.Message{Type: protocol.TypeStreamClose}, protocol.StreamClosePayload{StreamID: stream.ID, Reason: "the shell exited"})
		if got := stream.Reason(); got != "the shell exited" {
			t.Errorf("reason = %q", got)
		}
		if err := stream.Write(t.Context(), []byte("x")); !errors.Is(err, hub.ErrStreamClosed) {
			t.Errorf("Write after close = %v, want ErrStreamClosed", err)
		}
	})

	t.Run("tells the agent when the server closes it", func(t *testing.T) {
		h := newHarness(t)
		agent := connectedAgent(t, h)
		stream := openTerminal(t, h, agent)
		stream.Close(t.Context(), "closed by the browser")
		stream.Close(t.Context(), "twice is harmless")
		var closed protocol.StreamClosePayload
		agent.readPayload(protocol.TypeStreamClose, &closed)
		if closed.StreamID != stream.ID || closed.Reason != "closed by the browser" {
			t.Errorf("agent got %+v", closed)
		}
	})

	t.Run("asks the agent to keep the session when the viewer only went away", func(t *testing.T) {
		h := newHarness(t)
		agent := connectedAgent(t, h)
		stream := openTerminal(t, h, agent)
		stream.Detach(t.Context())
		var closed protocol.StreamClosePayload
		agent.readPayload(protocol.TypeStreamClose, &closed)
		if !closed.KeepSession || closed.StreamID != stream.ID {
			t.Errorf("agent got %+v, want keepSession", closed)
		}
	})

	t.Run("ends every stream when the connection ends", func(t *testing.T) {
		h := newHarness(t)
		agent := connectedAgent(t, h)
		stream := openTerminal(t, h, agent)
		_ = agent.conn.Close(websocket.StatusNormalClosure, "")
		if got := stream.Reason(); got != "the machine disconnected" {
			t.Errorf("reason = %q", got)
		}
	})

	t.Run("closes a stream whose reader falls too far behind, without stalling the connection", func(t *testing.T) {
		h := newHarness(t)
		agent := connectedAgent(t, h)
		stream := openTerminal(t, h, agent)
		for range 1100 { // more than the buffer, and nobody reading
			agent.sendFrame(stream.ID, "x")
		}
		if got := stream.Reason(); got != "the viewer couldn't keep up" {
			t.Errorf("reason = %q", got)
		}
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		for {
			message := agent.read()
			if message.Type == protocol.TypePong {
				break
			}
		}
	})

	t.Run("ignores frames for unknown streams and malformed frames", func(t *testing.T) {
		h := newHarness(t)
		agent := connectedAgent(t, h)
		agent.sendFrame(99, "lost")
		if err := agent.conn.Write(t.Context(), websocket.MessageBinary, []byte{1}); err != nil {
			t.Fatal(err)
		}
		agent.send(protocol.Message{Type: protocol.TypeStreamClose}, map[string]string{"streamId": "bad"})
		agent.send(protocol.Message{Type: protocol.TypeStreamClose}, protocol.StreamClosePayload{StreamID: 98})
		agent.send(protocol.Message{Type: protocol.TypePing}, nil)
		if pong := agent.read(); pong.Type != protocol.TypePong {
			t.Errorf("got %s, want the connection still working", pong.Type)
		}
	})

	t.Run("fails to open on an offline machine or when the agent refuses", func(t *testing.T) {
		h := newHarness(t)
		noPayload := func(protocol.StreamID) any { return nil }
		if _, err := h.hub.OpenStream(t.Context(), 7, protocol.ActionTerminalOpen, noPayload, nil); !errors.Is(err, hub.ErrOffline) {
			t.Errorf("offline: err = %v", err)
		}
		agent := connectedAgent(t, h)
		result := make(chan error, 1)
		go func() {
			_, err := h.hub.OpenStream(context.Background(), h.agentID(t, agent), protocol.ActionTerminalOpen, noPayload, nil)
			result <- err
		}()
		request := agent.read()
		agent.send(protocol.Message{Type: protocol.TypeResponse, ID: request.ID,
			Error: &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "Terminal is disabled by local policy"}}, nil)
		var agentErr *protocol.Error
		if err := <-result; !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied {
			t.Errorf("refused: err = %v", err)
		}
	})
}

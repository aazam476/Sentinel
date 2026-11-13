// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// streamBuffer is how many frames from the agent a stream holds for a slow
// reader. A terminal frame is at most a few kilobytes, so this is a few
// megabytes at worst.
const streamBuffer = 1024

// Stream is one stream (§7.6) between the server and an agent: a terminal
// session, a file transfer. Data from the agent arrives on Frames; Write
// sends data to the agent.
type Stream struct {
	ID   protocol.StreamID
	conn *connection

	frames chan []byte
	done   chan struct{}
	once   sync.Once
	// reason says why the stream ended; set once, before done closes.
	reason string
}

// ErrStreamClosed means the stream ended. It is never reassigned.
var ErrStreamClosed = errors.New("the stream is closed")

// OpenStream starts a stream with an agent. The server picks the stream ID
// and registers the stream before asking, so no data the agent sends right
// after accepting can be lost. payload builds the request's payload around
// the ID; result receives the agent's response.
func (h *Hub) OpenStream(ctx context.Context, agentID int64, action string, payload func(protocol.StreamID) any, result any) (*Stream, error) {
	conn, ok := h.connectionOf(agentID)
	if !ok {
		return nil, ErrOffline
	}
	stream, err := conn.addStream()
	if err != nil {
		return nil, err
	}
	if err := h.request(ctx, conn, RequestTimeout, action, payload(stream.ID), result); err != nil {
		stream.end("the agent didn't start it")
		return nil, err
	}
	return stream, nil
}

// Frames delivers the agent's data, in order. It isn't closed; watch Done,
// and read what is still buffered once Done closes.
func (s *Stream) Frames() <-chan []byte {
	return s.frames
}

// Done is closed when the stream ends, from either side.
func (s *Stream) Done() <-chan struct{} {
	return s.done
}

// Reason says why the stream ended, once Done is closed.
func (s *Stream) Reason() string {
	<-s.done
	return s.reason
}

// Write sends data to the agent on this stream.
func (s *Stream) Write(ctx context.Context, data []byte) error {
	select {
	case <-s.done:
		return ErrStreamClosed
	default:
	}
	// A cancelled context mid-write would make the WebSocket library
	// close the whole connection, ending every other stream on it: so a
	// caller that has given up stops here, and a write that has begun
	// only stops at the time limit.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("not sent: %w", err)
	}
	ctx, cancel := clock.WithTimeout(context.WithoutCancel(ctx), s.conn.hub.clock, writeTimeout)
	defer cancel()
	return protocol.WriteFrame(ctx, s.conn.conn, s.ID, data)
}

// Close ends the stream and tells the agent why.
func (s *Stream) Close(ctx context.Context, reason string) {
	s.closeWith(ctx, protocol.StreamClosePayload{StreamID: s.ID, Reason: reason})
}

// Detach ends the stream because the viewer went away, asking the agent to
// keep what runs behind it (a terminal's shell) so a new stream can
// reattach to it.
func (s *Stream) Detach(ctx context.Context) {
	s.closeWith(ctx, protocol.StreamClosePayload{StreamID: s.ID, Reason: "the viewer disconnected", KeepSession: true})
}

func (s *Stream) closeWith(ctx context.Context, payload protocol.StreamClosePayload) {
	if !s.end(payload.Reason) {
		return
	}
	message := protocol.Message{Type: protocol.TypeStreamClose}
	// Best effort: if the connection is gone, so is the stream on the agent.
	_ = s.conn.send(ctx, message, payload)
}

// end marks the stream ended and forgets it; it reports whether this call
// ended it.
func (s *Stream) end(reason string) bool {
	ended := false
	s.once.Do(func() {
		s.conn.removeStream(s.ID)
		s.reason = reason
		close(s.done)
		ended = true
	})
	return ended
}

// addStream registers a new stream with the next free ID.
func (c *connection) addStream() (*Stream, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.closed {
		return nil, ErrDisconnected
	}
	c.lastStreamID++
	stream := &Stream{
		ID: c.lastStreamID, conn: c,
		frames: make(chan []byte, streamBuffer), done: make(chan struct{}),
	}
	c.streams[stream.ID] = stream
	return stream, nil
}

func (c *connection) removeStream(id protocol.StreamID) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.streams, id)
}

func (c *connection) streamOf(id protocol.StreamID) (*Stream, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	stream, ok := c.streams[id]
	return stream, ok
}

// deliverFrame hands a frame from the agent to its stream. A stream whose
// reader has fallen too far behind is closed rather than allowed to stall
// the whole connection, which carries every other stream too.
func (c *connection) deliverFrame(ctx context.Context, frame []byte) {
	id, data, err := protocol.DecodeFrame(frame)
	if err != nil {
		c.hub.logger.Warn("agent sent a malformed stream frame", "agent", c.agent.PublicID, "error", err)
		return
	}
	stream, ok := c.streamOf(id)
	if !ok {
		return // a stream that just ended; its last data has nowhere to go
	}
	select {
	case stream.frames <- data:
	case <-stream.done:
	default:
		stream.Close(ctx, "the viewer couldn't keep up")
	}
}

// handleStreamClose ends a stream the agent closed.
func (c *connection) handleStreamClose(message protocol.Message) {
	var payload protocol.StreamClosePayload
	if err := protocol.DecodePayload(message, &payload); err != nil {
		c.hub.logger.Warn("agent sent a malformed stream.close", "agent", c.agent.PublicID, "error", err)
		return
	}
	if stream, ok := c.streamOf(payload.StreamID); ok {
		stream.end(payload.Reason)
	}
}

// endStreams ends every stream when the connection ends.
func (c *connection) endStreams() {
	c.mutex.Lock()
	streams := make([]*Stream, 0, len(c.streams))
	for _, stream := range c.streams {
		streams = append(streams, stream)
	}
	c.mutex.Unlock()
	for _, stream := range streams {
		stream.end("the machine disconnected")
	}
}

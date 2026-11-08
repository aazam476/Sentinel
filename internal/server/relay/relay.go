// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/coder/websocket"
)

// AgentStream is the agent's end of the pipe (hub.Stream).
type AgentStream interface {
	Frames() <-chan []byte
	Done() <-chan struct{}
	Reason() string
	Write(ctx context.Context, data []byte) error
	Close(ctx context.Context, reason string)
	Detach(ctx context.Context)
}

// Control is a message from the browser.
type Control struct {
	// Type is "resize" or "close" (the user closed the terminal).
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// Status is a message to the browser.
type Status struct {
	// Type is "opened" or "closed".
	Type      string `json:"type"`
	SessionID string `json:"sessionId,omitempty"`
	Resumed   bool   `json:"resumed,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Result is how a pipe ended.
type Result struct {
	// Reason says why it ended.
	Reason string
	// Detached is true when the browser went away without closing the
	// terminal, so the session can be resumed.
	Detached bool
	// BytesIn counts bytes from the browser; BytesOut, bytes to it.
	BytesIn, BytesOut int64
}

// Resize changes the terminal's size on the agent.
type Resize func(ctx context.Context, cols, rows uint16) error

// maxControlSize caps a control message; real ones are a few dozen bytes.
const maxControlSize = 64 << 10

// Pipe copies between browser and stream until either ends. It sends the
// browser opened first, and closed with the reason when the agent ends the
// stream.
func Pipe(ctx context.Context, browser *websocket.Conn, stream AgentStream, opened Status, resize Resize) Result {
	browser.SetReadLimit(maxControlSize)
	opened.Type = "opened"
	if err := writeStatus(ctx, browser, opened); err != nil {
		stream.Detach(ctx)
		return Result{Reason: "the browser went away", Detached: true}
	}
	// stopOutput ends copyToBrowser. When the user closes the terminal it
	// runs before the stream is closed, so copyToBrowser doesn't take the
	// stream's end for the agent's and close the browser itself: the
	// caller closes it, after recording the session's end.
	outputCtx, stopOutput := context.WithCancel(ctx)
	defer stopOutput()
	out := make(chan int64, 1)
	go func() { out <- copyToBrowser(outputCtx, browser, stream) }()
	result := copyToAgent(ctx, browser, stream, resize, stopOutput)
	stopOutput()
	result.BytesOut = <-out
	return result
}

// copyToAgent forwards the browser's input until the browser closes the
// terminal, goes away, or the stream ends.
func copyToAgent(ctx context.Context, browser *websocket.Conn, stream AgentStream, resize Resize, stopOutput func()) Result {
	var result Result
	for {
		kind, data, err := browser.Read(ctx)
		if err != nil {
			return endAfterReadError(ctx, stream, result)
		}
		if kind == websocket.MessageBinary {
			result.BytesIn += int64(len(data))
			if stream.Write(ctx, data) != nil {
				result.Reason = stream.Reason()
				return result
			}
			continue
		}
		var control Control
		if json.Unmarshal(data, &control) != nil {
			continue // not something this server sends or understands
		}
		switch control.Type {
		case "resize":
			_ = resize(ctx, control.Cols, control.Rows) // a failed resize leaves the old size, which still works
		case "close":
			stopOutput()
			stream.Close(ctx, "closed by the user")
			result.Reason = "closed by the user"
			return result
		}
	}
}

// endAfterReadError decides what a failed read means: the stream ended
// (copyToBrowser closed the browser), or the browser went away.
func endAfterReadError(ctx context.Context, stream AgentStream, result Result) Result {
	select {
	case <-stream.Done():
		result.Reason = stream.Reason()
	default:
		stream.Detach(context.WithoutCancel(ctx))
		result.Reason, result.Detached = "the browser went away", true
	}
	return result
}

// copyToBrowser forwards the agent's output, then, when the stream ends,
// whatever output is still buffered and the reason.
func copyToBrowser(ctx context.Context, browser *websocket.Conn, stream AgentStream) int64 {
	var sent int64
	for {
		select {
		case <-ctx.Done():
			return sent
		case data := <-stream.Frames():
			if browser.Write(ctx, websocket.MessageBinary, data) != nil {
				return sent
			}
			sent += int64(len(data))
		case <-stream.Done():
			if ctx.Err() != nil {
				return sent // the user closed it; the caller closes the browser
			}
			sent += drain(ctx, browser, stream)
			_ = writeStatus(ctx, browser, Status{Type: "closed", Reason: stream.Reason()}) // best effort
			_ = browser.Close(websocket.StatusNormalClosure, "")                           // ends copyToAgent's read
			return sent
		}
	}
}

func drain(ctx context.Context, browser *websocket.Conn, stream AgentStream) int64 {
	var sent int64
	for {
		select {
		case data := <-stream.Frames():
			if browser.Write(ctx, websocket.MessageBinary, data) != nil {
				return sent
			}
			sent += int64(len(data))
		default:
			return sent
		}
	}
}

func writeStatus(ctx context.Context, browser *websocket.Conn, status Status) error {
	data, err := json.Marshal(status)
	if err != nil {
		return errors.New("encode status")
	}
	return browser.Write(ctx, websocket.MessageText, data)
}

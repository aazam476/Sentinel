// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
)

// maxDesktopMessage caps a browser's message on a desktop session: an SDP
// offer is a few kilobytes, input events far less.
const maxDesktopMessage = 256 << 10

// PipeDesktop relays a remote desktop session (§9.3) until either end
// stops. The browser's text messages are control messages for the agent;
// the agent's control messages go back as text, and screen updates it
// sends over the server (when WebRTC can't connect) as binary messages.
func PipeDesktop(ctx context.Context, browser *websocket.Conn, stream AgentStream) Result {
	browser.SetReadLimit(maxDesktopMessage)
	// stop ends desktopToBrowser once the browser has gone. It is a
	// channel, not a context, as in Pipe: the WebSocket library closes the
	// connection when the context of a read or write ends, which would cut
	// the browser off before it hears why the session ended.
	stop := make(chan struct{})
	out := make(chan int64, 1)
	go func() { out <- desktopToBrowser(ctx, browser, stream, stop) }()
	var result Result
	result.Reason, result.BytesIn = desktopToAgent(ctx, browser, stream)
	close(stop)
	result.BytesOut = <-out
	if result.Reason == "" {
		result.Reason = stream.Reason()
	}
	stream.Close(context.WithoutCancel(ctx), "the viewer left")
	return result
}

// desktopToAgent passes the browser's control messages to the agent.
func desktopToAgent(ctx context.Context, browser *websocket.Conn, stream AgentStream) (string, int64) {
	var bytes int64
	for {
		kind, data, err := browser.Read(ctx)
		if err != nil {
			select {
			case <-stream.Done():
				return "", bytes // the agent ended it, and desktopToBrowser closed the browser
			default:
			}
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return "the viewer closed the session", bytes
			}
			return "the viewer went away", bytes
		}
		if kind != websocket.MessageText {
			continue
		}
		bytes += int64(len(data))
		if err := stream.Write(ctx, append([]byte{protocol.DesktopControl}, data...)); err != nil {
			return "", bytes
		}
	}
}

// desktopToBrowser passes the agent's messages to the browser. When the
// agent ends the session, it closes the browser with the reason, which
// ends desktopToAgent's read.
func desktopToBrowser(ctx context.Context, browser *websocket.Conn, stream AgentStream, stop <-chan struct{}) int64 {
	var bytes int64
	for {
		select {
		case <-stop:
			return bytes
		case <-stream.Done():
			_ = browser.Close(websocket.StatusNormalClosure, CloseReason(stream.Reason())) // best effort; the browser may be gone
			return bytes
		case data := <-stream.Frames():
			if len(data) == 0 {
				continue
			}
			kind := websocket.MessageText
			if data[0] == protocol.DesktopFrame {
				kind = websocket.MessageBinary
			} else if data[0] != protocol.DesktopControl {
				continue
			}
			bytes += int64(len(data) - 1)
			if err := browser.Write(ctx, kind, data[1:]); err != nil {
				return bytes
			}
		}
	}
}

// maxCloseReason is the most a WebSocket close reason may hold, in bytes.
const maxCloseReason = 123

// CloseReason makes reason a sentence the browser can show as a WebSocket
// close reason: capitalized, ended with a full stop, and short enough.
func CloseReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	first, size := utf8.DecodeRuneInString(reason)
	reason = string(unicode.ToUpper(first)) + reason[size:]
	if !strings.HasSuffix(reason, ".") {
		reason += "."
	}
	for len(reason) > maxCloseReason {
		_, size := utf8.DecodeLastRuneInString(reason)
		reason = reason[:len(reason)-size]
	}
	return reason
}

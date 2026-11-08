// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// ContainerLogsEnd is the "end" event of a followed log: why it stopped.
type ContainerLogsEnd struct {
	Reason string `json:"reason"`
}

// handleFollowLogs is GET .../containers/{container}/logs/follow: a
// server-sent event stream of the container's recent lines, then each new
// one as it is written ("line" events), and an "end" event when the
// container stops. Closing the stream stops following on the agent.
func (h *handlers) handleFollowLogs(w http.ResponseWriter, r *http.Request, session auth.Session) {
	request, ok := containerLogsRequest(w, r)
	if !ok {
		return
	}
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var recent protocol.ContainerLogsResultPayload
	stream, err := h.hub.OpenStream(r.Context(), agent.ID, protocol.ActionDockerContainerLogs, func(id protocol.StreamID) any {
		request.StreamID = id
		return request
	}, &recent)
	if err != nil {
		h.writeAgentError(w, err)
		return
	}
	defer stream.Close(context.WithoutCancel(r.Context()), "the viewer left")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	events := &eventStream{w: w, controller: http.NewResponseController(w)}
	for _, line := range recent.Lines {
		if !events.send("line", line) {
			return
		}
	}
	h.relayLogLines(r.Context(), events, stream)
}

// relayLogLines sends each followed line until the stream or the viewer
// ends.
func (h *handlers) relayLogLines(ctx context.Context, events *eventStream, stream *hub.Stream) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-stream.Frames():
			if !sendLogLine(events, frame) {
				return
			}
		case <-stream.Done():
			// Lines written just before the end may still be buffered.
			for {
				select {
				case frame := <-stream.Frames():
					if !sendLogLine(events, frame) {
						return
					}
					continue
				default:
				}
				break
			}
			events.send("end", ContainerLogsEnd{Reason: stream.Reason()})
			return
		}
	}
}

// sendLogLine sends one followed line; it reports false when the viewer
// has gone.
func sendLogLine(events *eventStream, frame []byte) bool {
	var line protocol.ContainerLogLine
	if json.Unmarshal(frame, &line) != nil {
		return true // not a line; the agent sends only lines
	}
	return events.send("line", line)
}

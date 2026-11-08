// SPDX-License-Identifier: Apache-2.0

package hub

// EventKind says what changed about an agent.
type EventKind string

// The kinds of event.
const (
	// EventOnline: the agent finished its handshake.
	EventOnline EventKind = "online"
	// EventOffline: the agent's connection ended.
	EventOffline EventKind = "offline"
	// EventInfoUpdated: the agent reported new details about its machine.
	EventInfoUpdated EventKind = "info_updated"
)

// Event says something changed about an agent.
type Event struct {
	AgentID int64
	Kind    EventKind
}

// eventBuffer is how many events a slow subscriber can fall behind by
// before it misses some.
const eventBuffer = 256

// Subscribe returns a channel that receives every Event from now on, and a
// function that stops the subscription. A subscriber that falls more than
// eventBuffer events behind misses the newest ones, so it should treat an
// event as "something changed" and re-read the state.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	events := make(chan Event, eventBuffer)
	h.mutex.Lock()
	h.nextSubscriber++
	id := h.nextSubscriber
	h.subscribers[id] = events
	h.mutex.Unlock()
	return events, func() {
		h.mutex.Lock()
		defer h.mutex.Unlock()
		delete(h.subscribers, id)
	}
}

// publish is publishLocked for callers that don't hold h.mutex.
func (h *Hub) publish(event Event) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.publishLocked(event)
}

// publishLocked sends event to every subscriber. The caller holds h.mutex,
// so events arrive in the order the changes happened.
func (h *Hub) publishLocked(event Event) {
	for _, events := range h.subscribers {
		select {
		case events <- event:
		default: // this subscriber is too far behind; see Subscribe
		}
	}
}

// SPDX-License-Identifier: Apache-2.0

package terminal

// historySize is how much recent output a session keeps for a viewer that
// reattaches: enough to redraw a full screen of a busy terminal.
const historySize = 64 << 10

// history keeps the last historySize bytes written to it.
type history struct {
	data []byte
}

func (h *history) write(output []byte) {
	h.data = append(h.data, output...)
	if excess := len(h.data) - historySize; excess > 0 {
		// Copy rather than reslice, so the old bytes can be freed.
		h.data = append([]byte(nil), h.data[excess:]...)
	}
}

func (h *history) bytes() []byte {
	return append([]byte(nil), h.data...)
}

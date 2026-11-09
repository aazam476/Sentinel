// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"image"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Source is what a session shares: a screen, or the test pattern.
type Source interface {
	// Name says what is shared, e.g. "test pattern".
	Name() string
	// Size is the screen's size in pixels.
	Size() (width, height int)
	// Capture draws the screen into frame, which is Size() big.
	Capture(frame *image.RGBA) error
	// Input applies a mouse or keyboard event.
	Input(event protocol.DesktopInputEvent) error
	// Close releases the source.
	Close() error
}

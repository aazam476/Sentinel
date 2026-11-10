// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"context"
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

// Clipboard is a source whose text clipboard is shared with the browser.
// Sources without one simply don't share it.
type Clipboard interface {
	// SetClipboard puts text on the machine's clipboard, so pasting there
	// pastes it.
	SetClipboard(text string) error
	// WatchClipboard calls changed with the clipboard's text whenever
	// something on the machine copies, until ctx ends.
	WatchClipboard(ctx context.Context, changed func(text string))
}

// Monitors is a source that knows its monitors, so a session can show one
// of them instead of the whole desktop.
type Monitors interface {
	Monitors() ([]protocol.MonitorInfo, error)
}

// RegionCapturer is a source that can capture part of its screen
// directly; others are captured whole and cropped.
type RegionCapturer interface {
	// CaptureRegion draws the part of the screen at origin, frame's size,
	// into frame.
	CaptureRegion(frame *image.RGBA, origin image.Point) error
}

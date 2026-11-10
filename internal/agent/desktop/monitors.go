// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"context"
	"fmt"
	"image"
	"image/draw"

	"github.com/aazam476/sentinel/internal/protocol"
)

// allMonitors is the browser's choice of the whole desktop.
const allMonitors = -1

// announceMonitors tells the browser the machine's monitors, when it has
// more than one to choose from. The whole desktop is shown to start with.
func (s *Session) announceMonitors(ctx context.Context, out Output) {
	source, ok := s.source.(Monitors)
	if !ok {
		return
	}
	monitors, err := source.Monitors()
	if err != nil || len(monitors) < 2 {
		return
	}
	s.monitors = monitors
	shown := allMonitors
	sendControl(ctx, out, protocol.DesktopControlMessage{Type: protocol.DesktopMonitors, Monitors: monitors, Monitor: &shown})
}

// selectMonitor shows one monitor, or the whole desktop for -1 or a
// monitor the machine no longer has.
func (s *Session) selectMonitor(index *int) {
	s.region = image.Rectangle{}
	if index != nil && *index >= 0 && *index < len(s.monitors) {
		m := s.monitors[*index]
		s.region = image.Rect(m.X, m.Y, m.X+m.Width, m.Y+m.Height)
	}
	s.encoder.full = true
}

// shownSize is the size of what the browser sees: the chosen monitor, as
// far as it is still on the screen, or the whole screen.
func (s *Session) shownSize() (image.Rectangle, int, int) {
	width, height := s.source.Size()
	region := s.region.Intersect(image.Rect(0, 0, width, height))
	if region.Empty() {
		return image.Rectangle{}, width, height
	}
	return region, region.Dx(), region.Dy()
}

// capture fills s.frame with what the browser sees.
func (s *Session) capture() error {
	region, width, height := s.shownSize()
	if s.frame == nil || s.frame.Bounds().Dx() != width || s.frame.Bounds().Dy() != height {
		s.frame = image.NewRGBA(image.Rect(0, 0, width, height))
	}
	var err error
	switch source := s.source.(type) {
	case RegionCapturer:
		err = source.CaptureRegion(s.frame, region.Min)
	default:
		err = s.captureCropped(region)
	}
	if err != nil {
		return fmt.Errorf("capture the screen: %w", err)
	}
	return nil
}

// captureCropped captures the whole screen and keeps the region.
func (s *Session) captureCropped(region image.Rectangle) error {
	if region.Empty() {
		return s.source.Capture(s.frame)
	}
	width, height := s.source.Size()
	if s.whole == nil || s.whole.Bounds().Dx() != width || s.whole.Bounds().Dy() != height {
		s.whole = image.NewRGBA(image.Rect(0, 0, width, height))
	}
	if err := s.source.Capture(s.whole); err != nil {
		return err
	}
	draw.Draw(s.frame, s.frame.Bounds(), s.whole, region.Min, draw.Src)
	return nil
}

// toScreen moves a pointer event from the shown monitor's corner to the
// desktop's.
func (s *Session) toScreen(event protocol.DesktopInputEvent) protocol.DesktopInputEvent {
	region, _, _ := s.shownSize()
	switch event.Kind {
	case protocol.InputMouseMove, protocol.InputMouseDown, protocol.InputMouseUp, protocol.InputWheel:
		event.X += region.Min.X
		event.Y += region.Min.Y
	}
	return event
}

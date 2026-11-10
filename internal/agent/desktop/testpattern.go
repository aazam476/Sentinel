// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"sync"

	"github.com/aazam476/sentinel/internal/protocol"
)

// TestPattern is a synthetic screen for checking a connection end to end
// without a real display: colour bars, a square that moves each frame, the
// pointer, and a magenta mark wherever the browser clicked.
type TestPattern struct {
	width, height int

	mutex  sync.Mutex
	frame  int
	cursor image.Point
	marks  []image.Point
	typed  int
	// copied hears what the test pattern "copies": it answers each paste
	// with a copy of its own, so one paste checks both directions.
	copied func(text string)
}

// MarkColor is what a click leaves on the test pattern.
var markColor = color.RGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff} //nolint:gochecknoglobals // a constant colour; Go has no const structs

// markSize is the side of a click's mark, in pixels.
const markSize = 24

// NewTestPattern returns a test pattern of the given size.
func NewTestPattern(width, height int) *TestPattern {
	return &TestPattern{width: width, height: height}
}

// Name is "test pattern".
func (p *TestPattern) Name() string { return "test pattern" }

// Size is the pattern's size.
func (p *TestPattern) Size() (int, int) { return p.width, p.height }

// Capture draws the next frame.
func (p *TestPattern) Capture(frame *image.RGBA) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	bars := []color.RGBA{
		{0xc0, 0xc0, 0xc0, 0xff}, {0xc0, 0xc0, 0x00, 0xff}, {0x00, 0xc0, 0xc0, 0xff}, {0x00, 0xc0, 0x00, 0xff},
		{0xc0, 0x00, 0xc0, 0xff}, {0xc0, 0x00, 0x00, 0xff}, {0x00, 0x00, 0xc0, 0xff},
	}
	barWidth := (p.width + len(bars) - 1) / len(bars)
	for i, bar := range bars {
		draw.Draw(frame, image.Rect(i*barWidth, 0, (i+1)*barWidth, p.height), &image.Uniform{C: bar}, image.Point{}, draw.Src)
	}
	// The moving square shows frames are arriving; each key typed shifts its row.
	side := p.height / 8
	x := (p.frame * 8) % max(p.width-side, 1)
	y := (p.typed * side / 2) % max(p.height-side, 1)
	draw.Draw(frame, image.Rect(x, y, x+side, y+side), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	for _, mark := range p.marks {
		draw.Draw(frame, image.Rect(mark.X-markSize/2, mark.Y-markSize/2, mark.X+markSize/2, mark.Y+markSize/2),
			&image.Uniform{C: markColor}, image.Point{}, draw.Src)
	}
	draw.Draw(frame, image.Rect(p.cursor.X-2, p.cursor.Y-2, p.cursor.X+3, p.cursor.Y+3), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)
	p.frame++
	return nil
}

// Input moves the pointer, marks clicks and counts keys.
func (p *TestPattern) Input(event protocol.DesktopInputEvent) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	switch event.Kind {
	case protocol.InputMouseMove:
		p.cursor = image.Pt(event.X, event.Y)
	case protocol.InputMouseDown:
		p.cursor = image.Pt(event.X, event.Y)
		p.marks = append(p.marks, p.cursor)
	case protocol.InputKeyDown:
		p.typed++
	}
	return nil
}

// Close does nothing: a test pattern holds nothing.
func (p *TestPattern) Close() error { return nil }

// SetClipboard takes pasted text and copies it back, marked as the test
// pattern's.
func (p *TestPattern) SetClipboard(text string) error {
	p.mutex.Lock()
	copied := p.copied
	p.mutex.Unlock()
	if copied != nil {
		copied("Copied on the test pattern: " + text)
	}
	return nil
}

// WatchClipboard hears the test pattern's copies until ctx ends.
func (p *TestPattern) WatchClipboard(ctx context.Context, changed func(text string)) {
	p.mutex.Lock()
	p.copied = changed
	p.mutex.Unlock()
	<-ctx.Done()
	p.mutex.Lock()
	p.copied = nil
	p.mutex.Unlock()
}

// Monitors are the test pattern's two halves, as if it were two monitors
// side by side, so a session can switch between them.
func (p *TestPattern) Monitors() ([]protocol.MonitorInfo, error) {
	half := p.width / 2
	return []protocol.MonitorInfo{
		{Name: "Left half", Width: half, Height: p.height, Primary: true},
		{Name: "Right half", X: half, Width: p.width - half, Height: p.height},
	}, nil
}

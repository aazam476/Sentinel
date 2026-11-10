// SPDX-License-Identifier: Apache-2.0

package x11_test

import (
	"image"
	"testing"

	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xproto"

	"github.com/aazam476/sentinel/internal/agent/desktop/x11"
	"github.com/aazam476/sentinel/internal/protocol"
)

func TestMonitors(t *testing.T) {
	display := startXvfbSized(t, false, "800x600")
	source := open(t, display)
	conn, root := observer(t, display)
	x11.RegisterExtensions(conn)
	// Two virtual monitors side by side, as xrandr --setmonitor makes.
	for _, monitor := range []struct {
		name    string
		x       int16
		primary bool
	}{{"LEFT", 0, true}, {"RIGHT", 400, false}} {
		atom, err := xproto.InternAtom(conn, false, uint16(len(monitor.name)), monitor.name).Reply() //nolint:gosec // G115: short test names
		if err != nil {
			t.Fatal(err)
		}
		if err := randr.SetMonitorChecked(conn, root, randr.MonitorInfo{
			Name: atom.Atom, Primary: monitor.primary, X: monitor.x, Width: 400, Height: 600,
			WidthInMillimeters: 100, HeightInMillimeters: 150,
		}).Check(); err != nil {
			t.Skipf("this X server can't set monitors: %v", err)
		}
	}

	monitors, err := source.Monitors()
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]protocol.MonitorInfo{}
	for _, monitor := range monitors {
		found[monitor.Name] = monitor
	}
	if found["LEFT"] != (protocol.MonitorInfo{Name: "LEFT", Width: 400, Height: 600, Primary: true}) ||
		found["RIGHT"] != (protocol.MonitorInfo{Name: "RIGHT", X: 400, Width: 400, Height: 600}) {
		t.Errorf("monitors = %+v", monitors)
	}

	// A red square on the right monitor, captured from the right monitor alone.
	gc, err := xproto.NewGcontextId(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err := xproto.CreateGCChecked(conn, gc, xproto.Drawable(root), xproto.GcForeground, []uint32{0xff0000}).Check(); err != nil {
		t.Fatal(err)
	}
	if err := xproto.PolyFillRectangleChecked(conn, xproto.Drawable(root), gc, []xproto.Rectangle{{X: 500, Y: 100, Width: 20, Height: 20}}).Check(); err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 400, 600))
	if err := source.CaptureRegion(frame, image.Pt(400, 0)); err != nil {
		t.Fatal(err)
	}
	if got := frame.RGBAAt(110, 110); got.R != 0xff || got.G != 0 || got.B != 0 {
		t.Errorf("pixel = %v, want the red square at its place on the right monitor", got)
	}
}

// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"fmt"
	"image"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// maxStripBytes keeps each PutImage request under the core protocol's
// 256 KiB limit, without the BIG-REQUESTS extension.
const maxStripBytes = 200 << 10

// window is a window of the agent's own on the desktop, above every other
// and left alone by the window manager (override-redirect), showing a
// picture.
type window struct {
	conn    *xgb.Conn
	id      xproto.Window
	gc      xproto.Gcontext
	depth   byte
	picture *image.RGBA
}

// openWindow shows picture at frame, a rectangle of the screen.
func openWindow(conn *xgb.Conn, frame image.Rectangle, picture *image.RGBA) (*window, error) {
	screen := xproto.Setup(conn).DefaultScreen(conn)
	id, err := xproto.NewWindowId(conn)
	if err != nil {
		return nil, fmt.Errorf("make a window: %w", err)
	}
	mask := uint32(xproto.CwBackPixel | xproto.CwOverrideRedirect | xproto.CwEventMask)
	values := []uint32{screen.WhitePixel, 1, xproto.EventMaskExposure | xproto.EventMaskButtonPress}
	err = xproto.CreateWindowChecked(conn, screen.RootDepth, id, screen.Root,
		int16(frame.Min.X), int16(frame.Min.Y), uint16(frame.Dx()), uint16(frame.Dy()), //nolint:gosec // G115: on-screen sizes fit
		0, xproto.WindowClassInputOutput, screen.RootVisual, mask, values).Check()
	if err != nil {
		return nil, fmt.Errorf("make a window: %w", err)
	}
	w := &window{conn: conn, id: id, depth: screen.RootDepth, picture: picture}
	if w.gc, err = xproto.NewGcontextId(conn); err == nil {
		err = xproto.CreateGCChecked(conn, w.gc, xproto.Drawable(id), 0, nil).Check()
	}
	if err == nil {
		err = xproto.MapWindowChecked(conn, id).Check()
	}
	if err == nil {
		err = w.paint()
	}
	if err != nil {
		w.close()
		return nil, fmt.Errorf("show a window: %w", err)
	}
	return w, nil
}

// paint puts the picture in the window, in strips small enough for one
// request each. X takes 32-bit pixels as blue, green, red and a spare
// byte.
func (w *window) paint() error {
	bounds := w.picture.Bounds()
	rows := max(maxStripBytes/(4*bounds.Dx()), 1)
	for top := 0; top < bounds.Dy(); top += rows {
		strip := min(rows, bounds.Dy()-top)
		data := make([]byte, 0, 4*bounds.Dx()*strip)
		for y := top; y < top+strip; y++ {
			for x := range bounds.Dx() {
				pixel := w.picture.RGBAAt(x, y)
				data = append(data, pixel.B, pixel.G, pixel.R, 0)
			}
		}
		err := xproto.PutImageChecked(w.conn, xproto.ImageFormatZPixmap, xproto.Drawable(w.id), w.gc,
			uint16(bounds.Dx()), uint16(strip), 0, int16(top), 0, w.depth, data).Check() //nolint:gosec // G115: on-screen sizes fit
		if err != nil {
			return fmt.Errorf("draw a window: %w", err)
		}
	}
	return nil
}

// close takes the window away.
func (w *window) close() {
	_ = xproto.DestroyWindowChecked(w.conn, w.id).Check() // the connection closes next anyway
}

// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"errors"
	"fmt"
	"image"
	"net"
	"strconv"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Display is an X display to connect to.
type Display struct {
	// Number is the display's number: 0 for ":0".
	Number int
	// Socket is the display's Unix socket; empty means the usual
	// /tmp/.X11-unix/X<number>.
	Socket string
	// Cookie is the display's MIT-MAGIC-COOKIE-1 in hex; empty connects
	// without one, for a server started without access control.
	Cookie string
}

// Name is the display as people write it, ":0".
func (d Display) Name() string { return ":" + strconv.Itoa(d.Number) }

func (d Display) socket() string {
	if d.Socket != "" {
		return d.Socket
	}
	return "/tmp/.X11-unix/X" + strconv.Itoa(d.Number)
}

// XTEST event types: X's own KeyPress, KeyRelease, ButtonPress,
// ButtonRelease and MotionNotify.
const (
	keyPress      = xproto.KeyPress
	keyRelease    = xproto.KeyRelease
	buttonPress   = xproto.ButtonPress
	buttonRelease = xproto.ButtonRelease
	motionNotify  = xproto.MotionNotify
)

// ErrUnsupportedFormat means the screen's pixels aren't 32 bits with 8-bit
// red, green and blue, the format of every modern X server.
var ErrUnsupportedFormat = errors.New("the screen's pixel format isn't supported")

// Source shares an X display.
type Source struct {
	conn    *xgb.Conn
	root    xproto.Window
	display Display

	clipboardMutex sync.Mutex
	clipboard      *clipboard // opened on first use
}

// Open connects to the display and checks it can be captured and driven.
func Open(display Display) (*Source, error) {
	conn, err := dial(display)
	if err != nil {
		return nil, err
	}
	source := &Source{conn: conn, root: xproto.Setup(conn).DefaultScreen(conn).Root, display: display}
	if err := source.check(); err != nil {
		conn.Close()
		return nil, err
	}
	return source, nil
}

// dial connects to the display with its cookie, if it has one.
func dial(display Display) (*xgb.Conn, error) {
	netConn, err := (&net.Dialer{}).Dial("unix", display.socket())
	if err != nil {
		return nil, fmt.Errorf("connect to X display %s: %w", display.Name(), err)
	}
	var conn *xgb.Conn
	if display.Cookie == "" {
		conn, err = xgb.NewConnNet(netConn)
	} else {
		conn, err = xgb.NewConnNetWithCookieHex(netConn, display.Cookie)
	}
	if err != nil {
		_ = netConn.Close() // the connection never started
		return nil, fmt.Errorf("connect to X display %s: %w", display.Name(), err)
	}
	registerExtensions(conn)
	return conn, nil
}

// check makes sure the server has XTEST and a pixel format Capture reads.
func (s *Source) check() error {
	if !hasExtension(s.conn, extensionXTEST) {
		return fmt.Errorf("the X display %s has no XTEST extension for input", s.display.Name())
	}
	setup := xproto.Setup(s.conn)
	screen := setup.DefaultScreen(s.conn)
	if setup.ImageByteOrder != xproto.ImageOrderLSBFirst || screen.RootDepth < 24 {
		return ErrUnsupportedFormat
	}
	for _, format := range setup.PixmapFormats {
		if format.Depth == screen.RootDepth && format.BitsPerPixel == 32 {
			return nil
		}
	}
	return ErrUnsupportedFormat
}

// Name says what is shared.
func (s *Source) Name() string { return "X11 " + s.display.Name() }

// Size is the screen's current size, which changes when it is resized.
func (s *Source) Size() (int, int) {
	geometry, err := xproto.GetGeometry(s.conn, xproto.Drawable(s.root)).Reply()
	if err != nil {
		screen := xproto.Setup(s.conn).DefaultScreen(s.conn)
		return int(screen.WidthInPixels), int(screen.HeightInPixels)
	}
	return int(geometry.Width), int(geometry.Height)
}

// Capture copies the screen into frame.
func (s *Source) Capture(frame *image.RGBA) error {
	return s.CaptureRegion(frame, image.Point{})
}

// CaptureRegion copies the part of the screen at origin, frame's size,
// into frame. X sends 32-bit pixels as blue, green, red and an unused
// byte.
func (s *Source) CaptureRegion(frame *image.RGBA, origin image.Point) error {
	bounds := frame.Bounds()
	reply, err := xproto.GetImage(s.conn, xproto.ImageFormatZPixmap, xproto.Drawable(s.root), int16(origin.X), int16(origin.Y), //nolint:gosec // G115: screen coordinates fit
		uint16(bounds.Dx()), uint16(bounds.Dy()), ^uint32(0)).Reply() //nolint:gosec // G115: Size gave these, from X's own uint16s
	if err != nil {
		return fmt.Errorf("capture X display %s: %w", s.display.Name(), err)
	}
	pixels := frame.Pix
	if len(reply.Data) < len(pixels) {
		return fmt.Errorf("capture X display %s: %w", s.display.Name(), ErrUnsupportedFormat)
	}
	for i := 0; i+3 < len(pixels); i += 4 {
		pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = reply.Data[i+2], reply.Data[i+1], reply.Data[i], 0xff
	}
	return nil
}

// Close disconnects from the display.
func (s *Source) Close() error {
	s.clipboardMutex.Lock()
	if s.clipboard != nil {
		s.clipboard.close()
		s.clipboard = nil
	}
	s.clipboardMutex.Unlock()
	s.conn.Close()
	return nil
}

// Input moves the pointer, presses buttons and keys, or scrolls.
func (s *Source) Input(event protocol.DesktopInputEvent) error {
	switch event.Kind {
	case protocol.InputMouseMove:
		return s.fake(motionNotify, 0, event.X, event.Y)
	case protocol.InputMouseDown, protocol.InputMouseUp:
		return s.button(event)
	case protocol.InputWheel:
		return s.wheel(event)
	case protocol.InputKeyDown, protocol.InputKeyUp:
		return s.key(event)
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedInput, event.Kind)
	}
}

// ErrUnsupportedInput means an input event X11 has no use for, such as
// Ctrl+Alt+Del, or a key it doesn't know.
var ErrUnsupportedInput = errors.New("unsupported input")

func (s *Source) button(event protocol.DesktopInputEvent) error {
	if event.Button < 0 || event.Button > 2 {
		return fmt.Errorf("%w: button %d", ErrUnsupportedInput, event.Button)
	}
	if err := s.fake(motionNotify, 0, event.X, event.Y); err != nil {
		return err
	}
	kind := byte(buttonPress)
	if event.Kind == protocol.InputMouseUp {
		kind = buttonRelease
	}
	// The browser numbers left, middle, right from 0; X from 1.
	return s.fake(kind, byte(event.Button+1), event.X, event.Y)
}

// wheel scrolls one step per event: X scrolls with buttons 4 (up), 5
// (down), 6 (left) and 7 (right), pressed and released.
func (s *Source) wheel(event protocol.DesktopInputEvent) error {
	var button byte
	switch {
	case event.DeltaY < 0:
		button = 4
	case event.DeltaY > 0:
		button = 5
	case event.DeltaX < 0:
		button = 6
	case event.DeltaX > 0:
		button = 7
	default:
		return nil
	}
	if err := s.fake(buttonPress, button, event.X, event.Y); err != nil {
		return err
	}
	return s.fake(buttonRelease, button, event.X, event.Y)
}

func (s *Source) key(event protocol.DesktopInputEvent) error {
	code, ok := keycode(event.Code)
	if !ok {
		return fmt.Errorf("%w: key %q", ErrUnsupportedInput, event.Code)
	}
	kind := byte(keyPress)
	if event.Kind == protocol.InputKeyUp {
		kind = keyRelease
	}
	return s.fake(kind, code, 0, 0)
}

// fake sends one XTEST event and waits for X to accept it, so an error
// comes back here rather than piling up unread.
func (s *Source) fake(kind, detail byte, x, y int) error {
	err := xtest.FakeInputChecked(s.conn, kind, detail, 0, s.root, clamp16(x), clamp16(y), 0).Check()
	if err != nil {
		return fmt.Errorf("send input to X display %s: %w", s.display.Name(), err)
	}
	return nil
}

func clamp16(value int) int16 {
	return int16(max(min(value, 32767), -32768))
}

// The extensions this package uses, by the names xgb files them under.
const (
	extensionXTEST  = "XTEST"
	extensionXFIXES = "XFIXES"
	extensionRANDR  = "RANDR"
)

// extensionInits are the xgb Init functions of the extensions used.
func extensionInits() map[string]func(*xgb.Conn) error {
	return map[string]func(*xgb.Conn) error{extensionXTEST: xtest.Init, extensionXFIXES: xfixes.Init, extensionRANDR: randr.Init}
}

// extensions records which extensions' Init has run in this process.
// Each Init fills package-level tables that every xgb connection's reader
// reads without a lock, so it may run only once, on the first connection:
// later connections only learn the extensions' opcodes, which is the rest
// of what Init does.
var extensions = struct { //nolint:gochecknoglobals // guards xgb's own package-level tables
	sync.Mutex
	primed map[string]bool
}{primed: map[string]bool{}}

// registerExtensions makes the extensions this package uses known to conn.
func registerExtensions(conn *xgb.Conn) {
	extensions.Lock()
	defer extensions.Unlock()
	for name, initialize := range extensionInits() {
		if !extensions.primed[name] {
			extensions.primed[name] = initialize(conn) == nil
			continue
		}
		reply, err := xproto.QueryExtension(conn, uint16(len(name)), name).Reply() //nolint:gosec // G115: short names
		if err != nil || !reply.Present {
			continue
		}
		conn.ExtLock.Lock()
		conn.Extensions[name] = reply.MajorOpcode
		conn.ExtLock.Unlock()
	}
}

// hasExtension says whether the display offers an extension.
func hasExtension(conn *xgb.Conn, name string) bool {
	conn.ExtLock.RLock()
	defer conn.ExtLock.RUnlock()
	_, ok := conn.Extensions[name]
	return ok
}

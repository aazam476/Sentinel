// SPDX-License-Identifier: Apache-2.0

package x11_test

import (
	"bufio"
	"errors"
	"image"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/aazam476/sentinel/internal/agent/desktop/x11"
	"github.com/aazam476/sentinel/internal/protocol"
)

// TestMain silences xgb's own logging: connecting without a cookie, as
// these tests' servers allow, makes it complain about ~/.Xauthority.
func TestMain(m *testing.M) {
	xgb.Logger = log.New(io.Discard, "", 0)
	os.Exit(m.Run())
}

// startXvfb starts a virtual X server of 320×240 and returns its display.
// With a cookie, the server lets in only clients that present it. Xvfb
// writes its display number to file descriptor 3 once it is ready.
func startXvfb(t *testing.T, withCookie bool) x11.Display {
	t.Helper()
	return startXvfbSized(t, withCookie, "320x240")
}

// startXvfbSized is startXvfb with a screen of the given size, "WxH".
func startXvfbSized(t *testing.T, withCookie bool, size string) x11.Display {
	t.Helper()
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb isn't installed")
	}
	args := []string{"-displayfd", "3", "-screen", "0", size + "x24", "-nolisten", "tcp"}
	if withCookie {
		file := filepath.Join(t.TempDir(), "Xauthority")
		if err := os.WriteFile(file, authEntry(65535, "", "", "MIT-MAGIC-COOKIE-1", cookie), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-auth", file)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	server := exec.CommandContext(t.Context(), "Xvfb", args...)
	server.ExtraFiles = []*os.File{writer}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	_ = writer.Close() // Xvfb has its own copy
	line, err := bufio.NewReader(reader).ReadString('\n')
	_ = reader.Close()
	number, convErr := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || convErr != nil {
		t.Fatalf("Xvfb didn't start: %q %v", line, err)
	}
	display := x11.Display{Number: number}
	if withCookie {
		display.Cookie = cookieHex
	}
	return display
}

func open(t *testing.T, display x11.Display) *x11.Source {
	t.Helper()
	source, err := x11.Open(display)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	return source
}

// observer is a second client watching the display, as a test's eyes.
func observer(t *testing.T, display x11.Display) (*xgb.Conn, xproto.Window) {
	t.Helper()
	netConn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", "/tmp/.X11-unix/X"+strconv.Itoa(display.Number))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := xgb.NewConnNet(netConn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	return conn, xproto.Setup(conn).DefaultScreen(conn).Root
}

func TestCapture(t *testing.T) {
	display := startXvfb(t, false)
	source := open(t, display)
	if got := source.Name(); got != "X11 "+display.Name() {
		t.Errorf("Name = %q", got)
	}
	width, height := source.Size()
	if width != 320 || height != 240 {
		t.Fatalf("Size = %dx%d", width, height)
	}

	// Paint a red square at (40, 30), then capture it.
	conn, root := observer(t, display)
	gc, err := xproto.NewGcontextId(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err := xproto.CreateGCChecked(conn, gc, xproto.Drawable(root), xproto.GcForeground, []uint32{0xff0000}).Check(); err != nil {
		t.Fatal(err)
	}
	if err := xproto.PolyFillRectangleChecked(conn, xproto.Drawable(root), gc, []xproto.Rectangle{{X: 40, Y: 30, Width: 20, Height: 20}}).Check(); err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	if err := source.Capture(frame); err != nil {
		t.Fatal(err)
	}
	if got := frame.RGBAAt(50, 40); got.R != 0xff || got.G != 0 || got.B != 0 || got.A != 0xff {
		t.Errorf("pixel in the square = %v, want red", got)
	}
	if got := frame.RGBAAt(5, 5); got.R == 0xff && got.G == 0 && got.B == 0 {
		t.Error("the pixel outside the square is red too")
	}
}

func TestInput(t *testing.T) {
	display := startXvfb(t, false)
	source := open(t, display)
	conn, root := observer(t, display)
	pointer := func() *xproto.QueryPointerReply {
		t.Helper()
		reply, err := xproto.QueryPointer(conn, root).Reply()
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	keyDown := func(keycode byte) bool {
		t.Helper()
		reply, err := xproto.QueryKeymap(conn).Reply()
		if err != nil {
			t.Fatal(err)
		}
		return reply.Keys[keycode/8]&(1<<(keycode%8)) != 0
	}

	send := func(event protocol.DesktopInputEvent) {
		t.Helper()
		if err := source.Input(event); err != nil {
			t.Fatalf("%s: %v", event.Kind, err)
		}
	}
	send(protocol.DesktopInputEvent{Kind: protocol.InputMouseMove, X: 100, Y: 50})
	if got := pointer(); got.RootX != 100 || got.RootY != 50 {
		t.Errorf("pointer at %d,%d, want 100,50", got.RootX, got.RootY)
	}
	send(protocol.DesktopInputEvent{Kind: protocol.InputMouseDown, X: 120, Y: 60, Button: 2})
	if got := pointer(); got.Mask&xproto.ButtonMask3 == 0 || got.RootX != 120 {
		t.Errorf("right button not down at 120: %+v", got)
	}
	send(protocol.DesktopInputEvent{Kind: protocol.InputMouseUp, X: 120, Y: 60, Button: 2})
	if got := pointer(); got.Mask&xproto.ButtonMask3 != 0 {
		t.Error("right button still down")
	}
	for _, delta := range [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}, {0, 0}} {
		send(protocol.DesktopInputEvent{Kind: protocol.InputWheel, DeltaX: delta[0], DeltaY: delta[1], X: 10, Y: 10})
	}

	// KeyA is evdev's 30, X's 38, whatever the layout types there.
	send(protocol.DesktopInputEvent{Kind: protocol.InputKeyDown, Code: "KeyA", Key: "a"})
	if !keyDown(38) {
		t.Error("A isn't down")
	}
	send(protocol.DesktopInputEvent{Kind: protocol.InputKeyUp, Code: "KeyA", Key: "a"})
	if keyDown(38) {
		t.Error("A is still down")
	}
}

func TestInputRefusals(t *testing.T) {
	source := open(t, startXvfb(t, false))
	for _, event := range []protocol.DesktopInputEvent{
		{Kind: protocol.InputKeyDown, Code: "LaunchRocket"},
		{Kind: protocol.InputMouseDown, Button: 4},
		{Kind: protocol.InputCtrlAltDel},
	} {
		if err := source.Input(event); !errors.Is(err, x11.ErrUnsupportedInput) {
			t.Errorf("%+v: err = %v, want unsupported", event, err)
		}
	}
}

func TestOpen(t *testing.T) {
	t.Run("connects with the display's cookie", func(t *testing.T) {
		display := startXvfb(t, true)
		open(t, display)
	})

	t.Run("is refused with the wrong cookie", func(t *testing.T) {
		display := startXvfb(t, true)
		display.Cookie = strings.Repeat("00", 16)
		if _, err := x11.Open(display); err == nil {
			t.Error("connected with the wrong cookie")
		}
	})

	t.Run("says when there's no display", func(t *testing.T) {
		if _, err := x11.Open(x11.Display{Number: 7, Socket: filepath.Join(t.TempDir(), "X7")}); err == nil {
			t.Error("connected to nothing")
		}
	})
}

func TestKeyTable(t *testing.T) {
	source := open(t, startXvfb(t, false))
	codes := []string{"Escape", "Digit1", "Digit0", "KeyQ", "KeyP", "KeyZ", "KeyM", "F1", "F9", "F10", "F12",
		"Enter", "Space", "ShiftLeft", "ControlRight", "AltRight", "MetaLeft", "ArrowUp", "Delete", "Numpad0"}
	for _, code := range codes {
		for _, kind := range []string{protocol.InputKeyDown, protocol.InputKeyUp} {
			if err := source.Input(protocol.DesktopInputEvent{Kind: kind, Code: code}); err != nil {
				t.Errorf("%s %s: %v", code, kind, err)
			}
		}
	}
	if err := source.Input(protocol.DesktopInputEvent{Kind: protocol.InputKeyDown, Code: "F:"}); err == nil {
		t.Error(`"F:" is a key`)
	}
}

func TestScreenOnTheDisplayThePolicyNames(t *testing.T) {
	display := startXvfb(t, true)
	authority := filepath.Join(t.TempDir(), "Xauthority")
	if err := os.WriteFile(authority, authEntry(256, "host", strconv.Itoa(display.Number), "MIT-MAGIC-COOKIE-1", cookie), 0o600); err != nil {
		t.Fatal(err)
	}
	screen := x11.NewScreen(fakeLoginctl{fail: true}, display.Name(), authority)
	if got := screen.Availability(t.Context()); !got.Available {
		t.Fatalf("Availability = %+v", got)
	}
	source, err := screen.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	if source.Name() != "X11 "+display.Name() {
		t.Errorf("Name = %q", source.Name())
	}
	if _, err := x11.NewScreen(fakeLoginctl{fail: true}, "nonsense", "").Open(t.Context()); err == nil {
		t.Error("opened a display with a bad name")
	}
}

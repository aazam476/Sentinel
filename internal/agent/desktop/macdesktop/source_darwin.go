// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package macdesktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"

	"github.com/aazam476/sentinel/internal/protocol"
)

// cgPoint is CGPoint.
type cgPoint struct{ X, Y float64 }

// graphics are the CoreGraphics and Accessibility functions used.
type graphics struct {
	mainDisplay         func() uint32
	displayWidth        func(display uint32) uintptr
	createMouseEvent    func(source uintptr, kind uint32, at cgPoint, button uint32) uintptr
	createKeyboardEvent func(source uintptr, key uint16, down bool) uintptr
	createScrollEvent   func(source uintptr, units, count uint32, vertical, horizontal, third int32) uintptr
	postEvent           func(tap uint32, event uintptr)
	release             func(object uintptr)
	screenCapture       func() bool
	accessibility       func() bool
}

var (
	graphicsOnce sync.Once //nolint:gochecknoglobals // the frameworks are loaded once per process
	loaded       *graphics //nolint:gochecknoglobals // see above
	loadErr      error     //nolint:gochecknoglobals // see above
)

func loadGraphics() (*graphics, error) {
	graphicsOnce.Do(func() { loaded, loadErr = openGraphics() })
	return loaded, loadErr
}

func openGraphics() (*graphics, error) {
	coreGraphics, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("load CoreGraphics: %w", err)
	}
	services, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("load ApplicationServices: %w", err)
	}
	foundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("load CoreFoundation: %w", err)
	}
	g := &graphics{}
	purego.RegisterLibFunc(&g.mainDisplay, coreGraphics, "CGMainDisplayID")
	purego.RegisterLibFunc(&g.displayWidth, coreGraphics, "CGDisplayPixelsWide")
	purego.RegisterLibFunc(&g.createMouseEvent, coreGraphics, "CGEventCreateMouseEvent")
	purego.RegisterLibFunc(&g.createKeyboardEvent, coreGraphics, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&g.createScrollEvent, coreGraphics, "CGEventCreateScrollWheelEvent2")
	purego.RegisterLibFunc(&g.postEvent, coreGraphics, "CGEventPost")
	purego.RegisterLibFunc(&g.screenCapture, coreGraphics, "CGPreflightScreenCaptureAccess")
	purego.RegisterLibFunc(&g.accessibility, services, "AXIsProcessTrusted")
	purego.RegisterLibFunc(&g.release, foundation, "CFRelease")
	return g, nil
}

func (g *graphics) screenCaptureAllowed() bool { return g.screenCapture() }
func (g *graphics) accessibilityAllowed() bool { return g.accessibility() }

// ErrNotPermitted means macOS hasn't let the agent record the screen.
var ErrNotPermitted = errors.New("macOS hasn't allowed sentinel-agent to record the screen: allow it under System Settings, Privacy & Security, Screen Recording and Accessibility")

// Source shares the Mac's main display.
type Source struct {
	graphics *graphics
	folder   string

	mutex   sync.Mutex
	width   int // of the last capture, in pixels
	height  int
	pressed map[int]bool
}

// Open checks the agent may record the screen and takes a first look.
func Open() (*Source, error) {
	g, err := loadGraphics()
	if err != nil {
		return nil, err
	}
	if !g.screenCaptureAllowed() {
		return nil, ErrNotPermitted
	}
	folder, err := os.MkdirTemp("", "sentinel-desktop-")
	if err != nil {
		return nil, fmt.Errorf("make a folder for captures: %w", err)
	}
	s := &Source{graphics: g, folder: folder, pressed: map[int]bool{}}
	if _, err := s.grab(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Name says what is shared.
func (s *Source) Name() string { return "macOS screen" }

// Size is the screen's size in pixels, as the last capture found it.
func (s *Source) Size() (int, int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.width, s.height
}

// grab captures the main display with screencapture (-x: no sound).
func (s *Source) grab() (image.Image, error) {
	file := filepath.Join(s.folder, "screen.png")
	output, err := exec.CommandContext(context.Background(), "/usr/sbin/screencapture", "-x", "-m", "-t", "png", file).CombinedOutput() //nolint:gosec // G204: the system's tool, into our own folder
	if err != nil {
		return nil, fmt.Errorf("capture the screen: %w: %s", err, bytes.TrimSpace(output))
	}
	data, err := os.ReadFile(file) //nolint:gosec // G304: our own capture
	if err != nil {
		return nil, fmt.Errorf("capture the screen: %w", err)
	}
	picture, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("capture the screen: %w", err)
	}
	s.mutex.Lock()
	s.width, s.height = picture.Bounds().Dx(), picture.Bounds().Dy()
	s.mutex.Unlock()
	return picture, nil
}

// Capture fills frame with the screen. A screen that changed size fills
// what overlaps; the next frame has the new size.
func (s *Source) Capture(frame *image.RGBA) error {
	picture, err := s.grab()
	if err != nil {
		return err
	}
	draw.Draw(frame, frame.Bounds(), picture, picture.Bounds().Min, draw.Src)
	return nil
}

// Close removes the capture folder.
func (s *Source) Close() error {
	return os.RemoveAll(s.folder)
}

// SetClipboard puts text on the clipboard with pbcopy.
func (s *Source) SetClipboard(text string) error {
	command := exec.CommandContext(context.Background(), "/usr/bin/pbcopy")
	command.Stdin = strings.NewReader(text)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("set the clipboard: %w: %s", err, bytes.TrimSpace(output))
	}
	return nil
}

// WatchClipboard checks the clipboard (pbpaste) twice a second and calls
// changed when its text changes, until ctx ends.
func (s *Source) WatchClipboard(ctx context.Context, changed func(text string)) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	last, _ := paste(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if text, err := paste(ctx); err == nil && text != last {
			last = text
			changed(text)
		}
	}
}

func paste(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/pbpaste").Output()
	if err != nil {
		return "", fmt.Errorf("read the clipboard: %w", err)
	}
	return string(output), nil
}

// ErrUnsupportedInput means an event macOS has no use for, or a key it
// doesn't know.
var ErrUnsupportedInput = errors.New("unsupported input")

// Input sends a mouse or keyboard event with CGEvent.
func (s *Source) Input(event protocol.DesktopInputEvent) error {
	if !s.graphics.accessibilityAllowed() {
		return errors.New("macOS hasn't allowed sentinel-agent to control the Mac: allow it under System Settings, Privacy & Security, Accessibility")
	}
	switch event.Kind {
	case protocol.InputMouseMove, protocol.InputMouseDown, protocol.InputMouseUp:
		return s.mouse(event)
	case protocol.InputWheel:
		return s.post(s.graphics.createScrollEvent(0, 0, 2, int32(-event.DeltaY*3), int32(-event.DeltaX*3), 0)) //nolint:gosec // G115: a few lines
	case protocol.InputKeyDown, protocol.InputKeyUp:
		key, ok := keyCode(event.Code)
		if !ok {
			return fmt.Errorf("%w: key %q", ErrUnsupportedInput, event.Code)
		}
		return s.post(s.graphics.createKeyboardEvent(0, key, event.Kind == protocol.InputKeyDown))
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedInput, event.Kind)
	}
}

// CGEvent mouse event types: down, up and dragged for the left, right
// and other buttons, and plain moves.
const (
	eventMoved = 5
	tapHID     = 0
)

func mouseEventTypes() map[int][3]uint32 { // button -> down, up, dragged
	return map[int][3]uint32{0: {1, 2, 6}, 1: {25, 26, 27}, 2: {3, 4, 7}}
}

func (s *Source) mouse(event protocol.DesktopInputEvent) error {
	if event.Button < 0 || event.Button > 2 {
		return fmt.Errorf("%w: button %d", ErrUnsupportedInput, event.Button)
	}
	// The capture is in pixels; events take points, which Retina screens
	// have half as many of.
	s.mutex.Lock()
	scale := 1.0
	if s.width > 0 {
		scale = float64(s.graphics.displayWidth(s.graphics.mainDisplay())) / float64(s.width)
	}
	types := mouseEventTypes()[event.Button]
	kind := uint32(eventMoved)
	switch event.Kind {
	case protocol.InputMouseDown:
		kind, s.pressed[event.Button] = types[0], true
	case protocol.InputMouseUp:
		kind, s.pressed[event.Button] = types[1], false
	default:
		for button, down := range s.pressed {
			if down {
				kind, event.Button = mouseEventTypes()[button][2], button
			}
		}
	}
	s.mutex.Unlock()
	at := cgPoint{X: float64(event.X) * scale, Y: float64(event.Y) * scale}
	return s.post(s.graphics.createMouseEvent(0, kind, at, uint32(event.Button)))
}

func (s *Source) post(event uintptr) error {
	if event == 0 {
		return errors.New("macOS couldn't make the input event")
	}
	defer s.graphics.release(event)
	s.graphics.postEvent(tapHID, event)
	return nil
}

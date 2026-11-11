// SPDX-License-Identifier: Apache-2.0

//go:build windows

package windesktop

import (
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/aazam476/sentinel/internal/protocol"
)

// The Win32 functions used, loaded from the system's own DLLs.
var (
	user32   = windows.NewLazySystemDLL("user32.dll")   //nolint:gochecknoglobals // a DLL is loaded once per process
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")    //nolint:gochecknoglobals // see above
	kernel32 = windows.NewLazySystemDLL("kernel32.dll") //nolint:gochecknoglobals // see above
	sas      = windows.NewLazySystemDLL("sas.dll")      //nolint:gochecknoglobals // see above

	procGetDC               = user32.NewProc("GetDC")               //nolint:gochecknoglobals // see above
	procReleaseDC           = user32.NewProc("ReleaseDC")           //nolint:gochecknoglobals // see above
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")    //nolint:gochecknoglobals // see above
	procOpenInputDesktop    = user32.NewProc("OpenInputDesktop")    //nolint:gochecknoglobals // see above
	procSetThreadDesktop    = user32.NewProc("SetThreadDesktop")    //nolint:gochecknoglobals // see above
	procCloseDesktop        = user32.NewProc("CloseDesktop")        //nolint:gochecknoglobals // see above
	procSendInput           = user32.NewProc("SendInput")           //nolint:gochecknoglobals // see above
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors") //nolint:gochecknoglobals // see above
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")     //nolint:gochecknoglobals // see above
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")   //nolint:gochecknoglobals // see above
	procDeleteDC            = gdi32.NewProc("DeleteDC")             //nolint:gochecknoglobals // see above
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")     //nolint:gochecknoglobals // see above
	procSelectObject        = gdi32.NewProc("SelectObject")         //nolint:gochecknoglobals // see above
	procDeleteObject        = gdi32.NewProc("DeleteObject")         //nolint:gochecknoglobals // see above
	procBitBlt              = gdi32.NewProc("BitBlt")               //nolint:gochecknoglobals // see above
	procSendSAS             = sas.NewProc("SendSAS")                //nolint:gochecknoglobals // see above
)

// GetSystemMetrics indexes for the virtual screen, which spans every
// monitor and may start left of or above the main one.
const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

const (
	srcCopy           = 0x00CC0020
	captureBlt        = 0x40000000
	desktopAllAccess  = 0x01FF
	inputMouse        = 0
	inputKeyboard     = 1
	monitorPrimary    = 1
	dibRGBColors      = 0
	bitmapInfoSize    = 40
	monitorInfoExSize = 104
)

// Source shares the console session's desktop. Its methods attach the
// calling thread to the input desktop first, so it follows a UAC prompt
// or the login screen; callers keep to one locked OS thread.
type Source struct {
	desktop windows.Handle
}

// Open attaches to the input desktop.
func Open() (*Source, error) {
	s := &Source{}
	if err := s.attach(); err != nil {
		return nil, err
	}
	return s, nil
}

// attach moves this thread to the desktop now receiving input.
func (s *Source) attach() error {
	desktop, _, err := procOpenInputDesktop.Call(0, 0, desktopAllAccess)
	if desktop == 0 {
		return fmt.Errorf("open the input desktop: %w", err)
	}
	if ok, _, err := procSetThreadDesktop.Call(desktop); ok == 0 {
		_, _, _ = procCloseDesktop.Call(desktop)
		return fmt.Errorf("switch to the input desktop: %w", err)
	}
	if s.desktop != 0 {
		_, _, _ = procCloseDesktop.Call(uintptr(s.desktop)) // no longer this thread's
	}
	s.desktop = windows.Handle(desktop)
	return nil
}

// Name says what is shared.
func (s *Source) Name() string { return "Windows desktop" }

func metric(index uintptr) int {
	value, _, _ := procGetSystemMetrics.Call(index)
	return int(int32(value)) //nolint:gosec // G115: metrics are 32-bit, and may be negative
}

// virtualScreen is the rectangle every monitor shares, in screen
// coordinates.
func virtualScreen() image.Rectangle {
	x, y := metric(smXVirtualScreen), metric(smYVirtualScreen)
	return image.Rect(x, y, x+metric(smCXVirtualScreen), y+metric(smCYVirtualScreen))
}

// Size is the size of all the monitors together.
func (s *Source) Size() (int, int) {
	screen := virtualScreen()
	return screen.Dx(), screen.Dy()
}

// Capture copies the whole desktop into frame.
func (s *Source) Capture(frame *image.RGBA) error {
	return s.CaptureRegion(frame, image.Point{})
}

// bitmapInfoHeader is BITMAPINFOHEADER.
type bitmapInfoHeader struct {
	size                        uint32
	width, height               int32
	planes, bitCount            uint16
	compression, imageSize      uint32
	xPerMeter, yPerMeter        int32
	colorsUsed, colorsImportant uint32
}

// CaptureRegion copies the part of the desktop at origin into frame, with
// GDI: the screen into a top-down 32-bit bitmap, then BGRA to RGBA.
func (s *Source) CaptureRegion(frame *image.RGBA, origin image.Point) error {
	if err := s.attach(); err != nil {
		return err
	}
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	screen, _, err := procGetDC.Call(0)
	if screen == 0 {
		return fmt.Errorf("capture the screen: %w", err)
	}
	defer procReleaseDC.Call(0, screen) //nolint:errcheck // releasing never fails usefully
	memory, _, err := procCreateCompatibleDC.Call(screen)
	if memory == 0 {
		return fmt.Errorf("capture the screen: %w", err)
	}
	defer procDeleteDC.Call(memory)                                                                                        //nolint:errcheck // see above
	header := bitmapInfoHeader{size: bitmapInfoSize, width: int32(width), height: -int32(height), planes: 1, bitCount: 32} //nolint:gosec // G115: screen sizes fit
	var bits unsafe.Pointer
	bitmap, _, err := procCreateDIBSection.Call(screen, uintptr(unsafe.Pointer(&header)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0) //nolint:gosec // G103: Win32 takes the header's and the result's addresses
	if bitmap == 0 {
		return fmt.Errorf("capture the screen: %w", err)
	}
	defer procDeleteObject.Call(bitmap) //nolint:errcheck // see above
	previous, _, _ := procSelectObject.Call(memory, bitmap)
	defer procSelectObject.Call(memory, previous) //nolint:errcheck // see above
	corner := virtualScreen().Min.Add(origin)
	if ok, _, err := procBitBlt.Call(memory, 0, 0, uintptr(width), uintptr(height), screen,
		uintptr(corner.X), uintptr(corner.Y), srcCopy|captureBlt); ok == 0 {
		return fmt.Errorf("capture the screen: %w", err)
	}
	pixels := unsafe.Slice((*byte)(bits), width*height*4) //nolint:gosec // G103: the bitmap's pixels, which Windows owns
	for i := 0; i+3 < len(pixels) && i+3 < len(frame.Pix); i += 4 {
		frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2], frame.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], 0xff
	}
	return nil
}

// Close lets go of the desktop.
func (s *Source) Close() error {
	if s.desktop != 0 {
		_, _, _ = procCloseDesktop.Call(uintptr(s.desktop))
		s.desktop = 0
	}
	return nil
}

// ErrUnsupportedInput means an input event Windows has no use for, or a
// key it doesn't know.
var ErrUnsupportedInput = errors.New("unsupported input")

// Input passes a mouse or keyboard event to Windows, or Ctrl+Alt+Del.
func (s *Source) Input(event protocol.DesktopInputEvent) error {
	if err := s.attach(); err != nil {
		return err
	}
	switch event.Kind {
	case protocol.InputMouseMove, protocol.InputMouseDown, protocol.InputMouseUp, protocol.InputWheel:
		return sendMouse(event)
	case protocol.InputKeyDown, protocol.InputKeyUp:
		return sendKey(event)
	case protocol.InputCtrlAltDel:
		// SendSAS needs SYSTEM, which the helper is, and the
		// SoftwareSASGeneration policy, which allows services by default.
		if err := procSendSAS.Find(); err != nil {
			return fmt.Errorf("send Ctrl+Alt+Del: %w", err)
		}
		_, _, _ = procSendSAS.Call(0)
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedInput, event.Kind)
	}
}

// mouseInput and keyboardInput are INPUT with its union filled; the
// keyboard one is padded to the mouse one's size, which is INPUT's.
type mouseInput struct {
	kind             uint32
	dx, dy           int32
	mouseData, flags uint32
	time             uint32
	extraInfo        uintptr
}

type keyboardInput struct {
	kind        uint32
	vk, scan    uint16
	flags, time uint32
	extraInfo   uintptr
	_           [8]byte
}

// Mouse event flags.
const (
	mouseMove       = 0x0001
	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	mouseHWheel     = 0x1000
	mouseVirtual    = 0x4000
	mouseAbsolute   = 0x8000
	wheelDelta      = 120
)

func sendMouse(event protocol.DesktopInputEvent) error {
	screen := virtualScreen()
	// Absolute positions run from 0 to 65535 across the virtual screen.
	in := mouseInput{
		kind:  inputMouse,
		dx:    int32(event.X * 65535 / max(screen.Dx()-1, 1)), //nolint:gosec // G115: within 0..65535
		dy:    int32(event.Y * 65535 / max(screen.Dy()-1, 1)), //nolint:gosec // see above
		flags: mouseMove | mouseAbsolute | mouseVirtual,
	}
	buttons := map[string][3]uint32{
		protocol.InputMouseDown: {mouseLeftDown, mouseMiddleDown, mouseRightDown},
		protocol.InputMouseUp:   {mouseLeftUp, mouseMiddleUp, mouseRightUp},
	}
	switch event.Kind {
	case protocol.InputMouseDown, protocol.InputMouseUp:
		if event.Button < 0 || event.Button > 2 {
			return fmt.Errorf("%w: button %d", ErrUnsupportedInput, event.Button)
		}
		in.flags |= buttons[event.Kind][event.Button]
	case protocol.InputWheel:
		in.flags |= mouseWheel
		in.mouseData = uint32(int32(-event.DeltaY * wheelDelta)) //nolint:gosec // G115: a signed step, as Windows takes it
		if event.DeltaX != 0 {
			in.flags = in.flags&^mouseWheel | mouseHWheel
			in.mouseData = uint32(int32(event.DeltaX * wheelDelta)) //nolint:gosec // see above
		}
	}
	return send(unsafe.Pointer(&in)) //nolint:gosec // G103: SendInput takes the INPUT's address
}

// Keyboard event flags.
const (
	keyExtended = 0x0001
	keyUp       = 0x0002
	keyScancode = 0x0008
)

func sendKey(event protocol.DesktopInputEvent) error {
	scan, extended, ok := scancode(event.Code)
	if !ok {
		return fmt.Errorf("%w: key %q", ErrUnsupportedInput, event.Code)
	}
	in := keyboardInput{kind: inputKeyboard, scan: scan, flags: keyScancode}
	if extended {
		in.flags |= keyExtended
	}
	if event.Kind == protocol.InputKeyUp {
		in.flags |= keyUp
	}
	return send(unsafe.Pointer(&in)) //nolint:gosec // G103: SendInput takes the INPUT's address
}

func send(in unsafe.Pointer) error {
	sent, _, err := procSendInput.Call(1, uintptr(in), unsafe.Sizeof(mouseInput{}))
	if sent != 1 {
		return fmt.Errorf("send input: %w", err)
	}
	return nil
}

// monitorInfo is MONITORINFOEXW.
type monitorInfo struct {
	size          uint32
	monitor, work windows.Rect
	flags         uint32
	device        [32]uint16
}

// monitorList collects EnumDisplayMonitors' answers. The callback is made
// once: Windows callbacks can't be freed.
var monitorList = struct { //nolint:gochecknoglobals // see above
	sync.Mutex
	found    []protocol.MonitorInfo
	callback uintptr
}{}

func monitorFound(handle, _ uintptr, _ *windows.Rect, _ uintptr) uintptr {
	info := monitorInfo{size: monitorInfoExSize}
	if ok, _, _ := procGetMonitorInfo.Call(handle, uintptr(unsafe.Pointer(&info))); ok == 0 { //nolint:gosec // G103: Win32 fills the struct at this address
		return 1
	}
	origin := virtualScreen().Min
	r := info.monitor
	monitorList.found = append(monitorList.found, protocol.MonitorInfo{
		Name: strings.TrimPrefix(windows.UTF16ToString(info.device[:]), `\\.\`),
		X:    int(r.Left) - origin.X, Y: int(r.Top) - origin.Y,
		Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top), Primary: info.flags&monitorPrimary != 0,
	})
	return 1 // go on
}

// Monitors lists the monitors, placed on the virtual screen.
func (s *Source) Monitors() ([]protocol.MonitorInfo, error) {
	monitorList.Lock()
	defer monitorList.Unlock()
	if monitorList.callback == 0 {
		monitorList.callback = syscall.NewCallback(monitorFound)
	}
	monitorList.found = nil
	if ok, _, err := procEnumDisplayMonitors.Call(0, 0, monitorList.callback, 0); ok == 0 {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	return append([]protocol.MonitorInfo(nil), monitorList.found...), nil
}

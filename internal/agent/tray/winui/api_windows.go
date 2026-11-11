// SPDX-License-Identifier: Apache-2.0

package winui

import (
	"golang.org/x/sys/windows"
)

// Window messages and flags (WinUser.h, shellapi.h).
const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmContextMenu = 0x007b
	wmCommand     = 0x0111
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmApp         = 0x8000

	// The tray's own messages.
	msgIcon    = wmApp + 1 // a click on the icon
	msgRefresh = wmApp + 2 // the model changed
	msgNotify  = wmApp + 3 // a notification is waiting

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	niifInfo   = 0x01

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	mbYesNo         = 0x00000004
	mbIconQuestion  = 0x00000020
	mbDefButton2    = 0x00000100
	mbSystemModal   = 0x00001000
	mbSetForeground = 0x00010000
	mbTopmost       = 0x00040000
	idYes           = 6
	idNo            = 7

	smCxSmIcon = 49
)

// point is POINT.
type point struct{ x, y int32 }

// msg is MSG.
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

// wndClassEx is WNDCLASSEXW.
type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

// notifyIconData is NOTIFYICONDATAW, Vista and later.
type notifyIconData struct {
	size            uint32
	hwnd            uintptr
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            uintptr
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	timeoutOrVer    uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guid            windows.GUID
	balloonIcon     uintptr
}

// iconInfo is ICONINFO.
type iconInfo struct {
	isIcon   int32
	xHotspot uint32
	yHotspot uint32
	mask     uintptr
	color    uintptr
}

// api holds the functions the tray calls.
type api struct {
	registerClassEx, createWindowEx, defWindowProc, destroyWindow *windows.LazyProc
	getMessage, translateMessage, dispatchMessage, postMessage    *windows.LazyProc
	postQuitMessage, registerWindowMessage, setForegroundWindow   *windows.LazyProc
	createPopupMenu, appendMenu, trackPopupMenu, destroyMenu      *windows.LazyProc
	getCursorPos, messageBox, enumThreadWindows, getSystemMetrics *windows.LazyProc
	createIconIndirect, destroyIcon, shellNotifyIcon              *windows.LazyProc
	createBitmap, deleteObject                                    *windows.LazyProc
}

func loadAPI() api {
	user32 := windows.NewLazySystemDLL("user32.dll")
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	gdi32 := windows.NewLazySystemDLL("gdi32.dll")
	return api{
		registerClassEx: user32.NewProc("RegisterClassExW"), createWindowEx: user32.NewProc("CreateWindowExW"),
		defWindowProc: user32.NewProc("DefWindowProcW"), destroyWindow: user32.NewProc("DestroyWindow"),
		getMessage: user32.NewProc("GetMessageW"), translateMessage: user32.NewProc("TranslateMessage"),
		dispatchMessage: user32.NewProc("DispatchMessageW"), postMessage: user32.NewProc("PostMessageW"),
		postQuitMessage: user32.NewProc("PostQuitMessage"), registerWindowMessage: user32.NewProc("RegisterWindowMessageW"),
		setForegroundWindow: user32.NewProc("SetForegroundWindow"), createPopupMenu: user32.NewProc("CreatePopupMenu"),
		appendMenu: user32.NewProc("AppendMenuW"), trackPopupMenu: user32.NewProc("TrackPopupMenu"),
		destroyMenu: user32.NewProc("DestroyMenu"), getCursorPos: user32.NewProc("GetCursorPos"),
		messageBox: user32.NewProc("MessageBoxW"), enumThreadWindows: user32.NewProc("EnumThreadWindows"),
		getSystemMetrics: user32.NewProc("GetSystemMetrics"), createIconIndirect: user32.NewProc("CreateIconIndirect"),
		destroyIcon: user32.NewProc("DestroyIcon"), shellNotifyIcon: shell32.NewProc("Shell_NotifyIconW"),
		createBitmap: gdi32.NewProc("CreateBitmap"), deleteObject: gdi32.NewProc("DeleteObject"),
	}
}

// utf16 converts text for a Windows call; text with a NUL becomes empty.
func utf16(text string) *uint16 {
	converted, err := windows.UTF16PtrFromString(text)
	if err != nil {
		empty := uint16(0)
		return &empty
	}
	return converted
}

// copyUTF16 fills a fixed-size field, cutting text that doesn't fit.
func copyUTF16(field []uint16, text string) {
	encoded, err := windows.UTF16FromString(text)
	if err != nil {
		return
	}
	if len(encoded) > len(field) {
		encoded = append(encoded[:len(field)-1], 0)
	}
	copy(field, encoded)
}

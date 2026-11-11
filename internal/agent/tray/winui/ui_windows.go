// SPDX-License-Identifier: Apache-2.0

package winui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

// instanceName keeps one tray per signed-in user: the mutex is per
// session.
const instanceName = `Local\SentinelTray`

// className names the hidden window that owns the icon.
const className = "SentinelTrayWindow"

// ErrAlreadyRunning means this user's tray is already running. It is never
// reassigned.
var ErrAlreadyRunning = errors.New("the Sentinel tray is already running for this user")

// UI is the notification area icon. Make one with New, then call Run on
// the goroutine that should own the window.
type UI struct {
	api      api
	onAction func(tray.Action)
	instance windows.Handle
	// wndProc and enumProc are the callbacks Windows calls; each is made
	// once, as Windows callbacks can't be freed.
	wndProc, enumProc uintptr
	taskbarCreated    uint32
	icons             map[tray.Icon]uintptr

	mutex   sync.Mutex
	hwnd    uintptr
	model   tray.Model
	notices []string
}

// New makes the UI; onAction hears the person's menu picks. It fails with
// ErrAlreadyRunning if this user already has a tray.
func New(onAction func(tray.Action)) (*UI, error) {
	instance, err := windows.CreateMutex(nil, false, utf16(instanceName))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(instance) // not ours to keep
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		return nil, fmt.Errorf("check for another tray: %w", err)
	}
	u := &UI{api: loadAPI(), onAction: onAction, instance: instance, model: tray.Menu(tray.Status{}, false, time.Now())}
	u.wndProc = windows.NewCallback(u.windowProc)
	u.enumProc = windows.NewCallback(u.closeDialog)
	return u, nil
}

// Run shows the icon and handles its clicks until ctx ends.
func (u *UI) Run(ctx context.Context) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() { _ = windows.CloseHandle(u.instance) }() // the process is ending
	hwnd, err := u.createWindow()
	if err != nil {
		return err
	}
	u.icons = u.makeIcons()
	u.mutex.Lock()
	u.hwnd = hwnd
	u.mutex.Unlock()
	u.notifyIcon(nimAdd, nifMessage|nifIcon|nifTip)
	stop := context.AfterFunc(ctx, func() { u.post(wmClose) })
	defer stop()
	var message msg
	for {
		result, _, _ := u.api.getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0) //nolint:gosec // G103: Windows fills in the message
		if int32(result) <= 0 {                                                           //nolint:gosec // G115: GetMessage returns a BOOL, -1 on error
			break
		}
		_, _, _ = u.api.translateMessage.Call(uintptr(unsafe.Pointer(&message))) //nolint:gosec // G103: as above
		_, _, _ = u.api.dispatchMessage.Call(uintptr(unsafe.Pointer(&message)))  //nolint:gosec // G103: as above
	}
	for _, icon := range u.icons {
		_, _, _ = u.api.destroyIcon.Call(icon)
	}
	return nil
}

// createWindow registers the class and makes the hidden window.
func (u *UI) createWindow() (uintptr, error) {
	class := wndClassEx{wndProc: u.wndProc, className: utf16(className)}
	class.size = uint32(unsafe.Sizeof(class))
	if atom, _, err := u.api.registerClassEx.Call(uintptr(unsafe.Pointer(&class))); atom == 0 { //nolint:gosec // G103: Windows reads the class
		return 0, fmt.Errorf("register the tray window: %w", err)
	}
	created, _, _ := u.api.registerWindowMessage.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated")))) //nolint:gosec // G103: a string Windows reads
	u.taskbarCreated = uint32(created)                                                                  //nolint:gosec // G115: message IDs are 16 bits
	hwnd, _, err := u.api.createWindowEx.Call(0, uintptr(unsafe.Pointer(utf16(className))),             //nolint:gosec // G103: as above
		uintptr(unsafe.Pointer(utf16("Sentinel"))), 0, 0, 0, 0, 0, 0, 0, 0, 0) //nolint:gosec // G103: as above
	if hwnd == 0 {
		return 0, fmt.Errorf("make the tray window: %w", err)
	}
	return hwnd, nil
}

// windowProc handles the hidden window's messages.
func (u *UI) windowProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch uint32(message) { //nolint:gosec // G115: message IDs are 32 bits
	case msgIcon:
		switch lParam & 0xffff {
		case wmLButtonUp, wmRButtonUp, wmContextMenu:
			u.showMenu(hwnd)
		}
	case msgRefresh:
		u.notifyIcon(nimModify, nifIcon|nifTip)
	case msgNotify:
		u.showNotices()
	case wmClose:
		_, _, _ = u.api.destroyWindow.Call(hwnd)
	case wmDestroy:
		u.notifyIcon(nimDelete, 0)
		_, _, _ = u.api.postQuitMessage.Call(0)
	case u.taskbarCreated: // Explorer restarted: the icon is gone
		u.notifyIcon(nimAdd, nifMessage|nifIcon|nifTip)
	default:
		result, _, _ := u.api.defWindowProc.Call(hwnd, message, wParam, lParam)
		return result
	}
	return 0
}

// showMenu pops the menu up at the pointer and carries out the pick.
func (u *UI) showMenu(hwnd uintptr) {
	u.mutex.Lock()
	lines, actions := entries(u.model)
	u.mutex.Unlock()
	menu, _, _ := u.api.createPopupMenu.Call()
	defer func() { _, _, _ = u.api.destroyMenu.Call(menu) }()
	for _, line := range lines {
		switch {
		case line.separator:
			_, _, _ = u.api.appendMenu.Call(menu, mfSeparator, 0, 0)
		case !line.enabled:
			_, _, _ = u.api.appendMenu.Call(menu, mfString|mfGrayed, 0, uintptr(unsafe.Pointer(utf16(line.label)))) //nolint:gosec // G103: a string Windows copies
		default:
			_, _, _ = u.api.appendMenu.Call(menu, mfString, uintptr(line.command), uintptr(unsafe.Pointer(utf16(line.label)))) //nolint:gosec // G103: as above
		}
	}
	var at point
	_, _, _ = u.api.getCursorPos.Call(uintptr(unsafe.Pointer(&at))) //nolint:gosec // G103: Windows fills it in
	// The menu closes when clicked away from only if the window is in front.
	_, _, _ = u.api.setForegroundWindow.Call(hwnd)
	picked, _, _ := u.api.trackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify,
		uintptr(at.x), uintptr(at.y), 0, hwnd, 0) //nolint:gosec // G115: coordinates pass through as Windows ints
	_, _, _ = u.api.postMessage.Call(hwnd, wmNull, 0, 0)
	if action, ok := actions[uint16(picked)]; ok { //nolint:gosec // G115: command IDs are 16 bits
		u.onAction(action)
	}
}

// notifyIcon adds, changes or removes the icon.
func (u *UI) notifyIcon(operation uintptr, flags uint32) {
	u.mutex.Lock()
	data := notifyIconData{hwnd: u.hwnd, id: 1, flags: flags, callbackMessage: msgIcon, icon: u.icons[u.model.Icon]}
	copyUTF16(data.tip[:], tooltip(u.model.Tooltip))
	u.mutex.Unlock()
	data.size = uint32(unsafe.Sizeof(data))
	_, _, _ = u.api.shellNotifyIcon.Call(operation, uintptr(unsafe.Pointer(&data))) //nolint:gosec // G103: Windows reads it
}

// showNotices shows the waiting notifications as balloons.
func (u *UI) showNotices() {
	u.mutex.Lock()
	notices := u.notices
	u.notices = nil
	hwnd := u.hwnd
	u.mutex.Unlock()
	for _, notice := range notices {
		data := notifyIconData{hwnd: hwnd, id: 1, flags: nifInfo, infoFlags: niifInfo}
		data.size = uint32(unsafe.Sizeof(data))
		copyUTF16(data.info[:], notice)
		copyUTF16(data.infoTitle[:], "Sentinel")
		_, _, _ = u.api.shellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&data))) //nolint:gosec // G103: Windows reads it
	}
}

// post sends the window one of the tray's messages, once it exists.
func (u *UI) post(message uintptr) {
	u.mutex.Lock()
	hwnd := u.hwnd
	u.mutex.Unlock()
	if hwnd != 0 {
		_, _, _ = u.api.postMessage.Call(hwnd, message, 0, 0)
	}
}

// Update shows the model.
func (u *UI) Update(model tray.Model) {
	u.mutex.Lock()
	u.model = model
	u.mutex.Unlock()
	u.post(msgRefresh)
}

// Notify shows a balloon notification.
func (u *UI) Notify(text string) {
	u.mutex.Lock()
	u.notices = append(u.notices, text)
	u.mutex.Unlock()
	u.post(msgNotify)
}

// SPDX-License-Identifier: Apache-2.0

package winui

import (
	"context"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

// dismissInterval is how often a question that has gone away is closed
// again, in case its dialog wasn't on the screen yet.
const dismissInterval = 100 * time.Millisecond

// Ask shows a yes-or-no dialog in front of everything, No by default,
// until the person answers or ctx ends (SEC-23).
func (u *UI) Ask(ctx context.Context, question string) (bool, error) {
	answers, threads := make(chan uintptr, 1), make(chan uint32, 1)
	go func() {
		// The dialog belongs to this thread, which ends with the goroutine.
		runtime.LockOSThread()
		threads <- windows.GetCurrentThreadId()
		answer, _, _ := u.api.messageBox.Call(0, uintptr(unsafe.Pointer(utf16(question))), //nolint:gosec // G103: strings Windows reads
			uintptr(unsafe.Pointer(utf16("Sentinel"))), //nolint:gosec // G103: as above
			mbYesNo|mbIconQuestion|mbDefButton2|mbSystemModal|mbSetForeground|mbTopmost)
		answers <- answer
	}()
	thread := <-threads
	select {
	case answer := <-answers:
		return answer == idYes, nil
	case <-ctx.Done():
		go u.dismiss(thread, answers)
		return false, fmt.Errorf("the question went away: %w", context.Cause(ctx))
	}
}

// dismiss answers No for the person on a dialog that is no longer asked,
// until it has closed.
func (u *UI) dismiss(thread uint32, answers <-chan uintptr) {
	ticker := time.NewTicker(dismissInterval)
	defer ticker.Stop()
	for {
		_, _, _ = u.api.enumThreadWindows.Call(uintptr(thread), u.enumProc, 0)
		select {
		case <-answers:
			return
		case <-ticker.C:
		}
	}
}

// closeDialog presses No on one of the dialog thread's windows.
func (u *UI) closeDialog(hwnd, _ uintptr) uintptr {
	_, _, _ = u.api.postMessage.Call(hwnd, wmCommand, idNo, 0)
	return 1 // keep going
}

// makeIcons draws the icon for every state at the small icon size.
func (u *UI) makeIcons() map[tray.Icon]uintptr {
	size := 16
	if metric, _, _ := u.api.getSystemMetrics.Call(smCxSmIcon); metric > 0 && metric <= 256 {
		size = int(metric)
	}
	icons := map[tray.Icon]uintptr{}
	for _, state := range []tray.Icon{tray.IconOff, tray.IconIdle, tray.IconActive, tray.IconPaused} {
		if icon := u.makeIcon(state, size); icon != 0 {
			icons[state] = icon
		}
	}
	return icons
}

// makeIcon turns one drawing into an icon handle, or 0.
func (u *UI) makeIcon(state tray.Icon, size int) uintptr {
	img := tray.IconImage(state, size)
	pixels := make([]byte, 0, size*size*4)
	for i := 0; i < len(img.Pix); i += 4 { // RGBA to Windows' BGRA
		pixels = append(pixels, img.Pix[i+2], img.Pix[i+1], img.Pix[i], img.Pix[i+3])
	}
	// The mask is all zero: the colour bitmap's alpha decides.
	mask := make([]byte, (size+15)/16*2*size)
	side := uintptr(size)
	color, _, _ := u.api.createBitmap.Call(side, side, 1, 32, uintptr(unsafe.Pointer(&pixels[0])))   //nolint:gosec // G103: Windows copies the pixels
	monochrome, _, _ := u.api.createBitmap.Call(side, side, 1, 1, uintptr(unsafe.Pointer(&mask[0]))) //nolint:gosec // G103: as above
	defer func() {
		_, _, _ = u.api.deleteObject.Call(color)
		_, _, _ = u.api.deleteObject.Call(monochrome)
	}()
	if color == 0 || monochrome == 0 {
		return 0
	}
	info := iconInfo{isIcon: 1, mask: monochrome, color: color}
	icon, _, _ := u.api.createIconIndirect.Call(uintptr(unsafe.Pointer(&info))) //nolint:gosec // G103: Windows reads it
	return icon
}

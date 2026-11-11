// SPDX-License-Identifier: Apache-2.0

//go:build windows

package windesktop

import (
	"context"
	"fmt"
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procOpenClipboard              = user32.NewProc("OpenClipboard")              //nolint:gochecknoglobals // see source_windows.go
	procCloseClipboard             = user32.NewProc("CloseClipboard")             //nolint:gochecknoglobals // see above
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")             //nolint:gochecknoglobals // see above
	procGetClipboardData           = user32.NewProc("GetClipboardData")           //nolint:gochecknoglobals // see above
	procSetClipboardData           = user32.NewProc("SetClipboardData")           //nolint:gochecknoglobals // see above
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber") //nolint:gochecknoglobals // see above
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")              //nolint:gochecknoglobals // see above
	procGlobalFree                 = kernel32.NewProc("GlobalFree")               //nolint:gochecknoglobals // see above
	procGlobalLock                 = kernel32.NewProc("GlobalLock")               //nolint:gochecknoglobals // see above
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")             //nolint:gochecknoglobals // see above
)

const (
	unicodeText   = 13 // CF_UNICODETEXT
	globalMovable = 0x0002
	// clipboardPoll is how often the clipboard is checked for a copy.
	clipboardPoll = 500 * time.Millisecond
)

// openClipboard opens the clipboard, trying again while another program
// has it open.
func openClipboard() error {
	for range 10 {
		if ok, _, _ := procOpenClipboard.Call(0); ok != 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond) //nolint:forbidigo // the clipboard has no "free" notification; real time, not the agent's clock
	}
	return fmt.Errorf("open the clipboard: %w", windows.ERROR_ACCESS_DENIED)
}

// SetClipboard puts text on the clipboard, as Unicode text.
func (s *Source) SetClipboard(text string) error {
	if err := s.attach(); err != nil {
		return err
	}
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call() //nolint:errcheck // closing never fails usefully
	units := append(utf16.Encode([]rune(text)), 0)
	size := uintptr(len(units) * 2)
	memory, _, err := procGlobalAlloc.Call(globalMovable, size)
	if memory == 0 {
		return fmt.Errorf("set the clipboard: %w", err)
	}
	pointer, _, err := procGlobalLock.Call(memory)
	if pointer == 0 {
		_, _, _ = procGlobalFree.Call(memory)
		return fmt.Errorf("set the clipboard: %w", err)
	}
	copy(unsafe.Slice((*uint16)(windowsMemory(pointer)), len(units)), units) //nolint:gosec // G103: the memory GlobalLock gave
	_, _, _ = procGlobalUnlock.Call(memory)
	_, _, _ = procEmptyClipboard.Call()
	if ok, _, err := procSetClipboardData.Call(unicodeText, memory); ok == 0 {
		_, _, _ = procGlobalFree.Call(memory) // the clipboard didn't take it
		return fmt.Errorf("set the clipboard: %w", err)
	}
	return nil
}

// clipboardText reads the clipboard's Unicode text.
func clipboardText() (string, bool) {
	if openClipboard() != nil {
		return "", false
	}
	defer procCloseClipboard.Call() //nolint:errcheck // see above
	memory, _, _ := procGetClipboardData.Call(unicodeText)
	if memory == 0 {
		return "", false
	}
	pointer, _, _ := procGlobalLock.Call(memory)
	if pointer == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(memory) //nolint:errcheck // see above
	return windows.UTF16PtrToString((*uint16)(windowsMemory(pointer))), true
}

// WatchClipboard calls changed with the clipboard's text after each copy,
// until ctx ends. Windows numbers every change; this checks the number.
func (s *Source) WatchClipboard(ctx context.Context, changed func(text string)) {
	runtime.LockOSThread() // the clipboard belongs to this thread's desktop
	defer runtime.UnlockOSThread()
	watcher := &Source{}
	defer func() { _ = watcher.Close() }()
	ticker := time.NewTicker(clipboardPoll)
	defer ticker.Stop()
	last, _, _ := procGetClipboardSequenceNumber.Call()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if watcher.attach() != nil {
			continue
		}
		sequence, _, _ := procGetClipboardSequenceNumber.Call()
		if sequence == last {
			continue
		}
		last = sequence
		if text, ok := clipboardText(); ok {
			changed(text)
		}
	}
}

// windowsMemory turns an address GlobalLock returned into a pointer. The
// memory is Windows', not Go's, so the garbage collector never moves it.
func windowsMemory(address uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&address)) //nolint:gosec // G103: see above
}

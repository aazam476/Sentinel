// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"runtime"

	"github.com/aazam476/sentinel/internal/agent/tray"
	"github.com/aazam476/sentinel/internal/agent/tray/macui"
)

// AppKit only works on the process's main thread, and only an init
// function runs early enough to keep main on it.
func init() { //nolint:gochecknoinits // see above: there is no other way to hold the main thread
	runtime.LockOSThread()
}

// newTrayUI is the menu bar item.
func newTrayUI(onAction func(tray.Action)) (trayUI, error) {
	ui, err := macui.New(onAction)
	if errors.Is(err, macui.ErrAlreadyRunning) {
		return nil, errTrayRunning
	}
	if err != nil {
		return nil, err //nolint:wrapcheck // macui's errors say what failed
	}
	return ui, nil
}

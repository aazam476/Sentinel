// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !darwin

package main

import (
	"errors"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

// newTrayUI has no tray to show: on Linux, consent prompts and the session
// indicator appear on the X11 screen itself.
func newTrayUI(func(tray.Action)) (trayUI, error) {
	return nil, errors.New("the tray is for Windows and macOS; on Linux the agent asks and shows sessions on the X11 screen")
}

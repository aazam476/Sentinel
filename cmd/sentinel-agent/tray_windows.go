// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"

	"github.com/aazam476/sentinel/internal/agent/tray"
	"github.com/aazam476/sentinel/internal/agent/tray/winui"
)

// newTrayUI is the notification area icon.
func newTrayUI(onAction func(tray.Action)) (trayUI, error) {
	ui, err := winui.New(onAction)
	if errors.Is(err, winui.ErrAlreadyRunning) {
		return nil, errTrayRunning
	}
	if err != nil {
		return nil, err //nolint:wrapcheck // winui's errors say what failed
	}
	return ui, nil
}

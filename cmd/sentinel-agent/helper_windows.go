// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import (
	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/desktop/windesktop"
)

// openHelperSource is the desktop of the session the helper runs in.
func openHelperSource() (desktop.Source, error) {
	return windesktop.Open()
}

// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package main

import (
	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/desktop/macdesktop"
)

// openHelperSource is the screen of the session the helper runs in.
func openHelperSource() (desktop.Source, error) {
	return macdesktop.Open()
}

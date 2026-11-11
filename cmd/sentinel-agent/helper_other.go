// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !darwin

package main

import (
	"errors"

	"github.com/aazam476/sentinel/internal/agent/desktop"
)

// openHelperSource has nothing to open: on Linux the agent reaches the X
// display itself and needs no helper.
func openHelperSource() (desktop.Source, error) {
	return nil, errors.New("the desktop helper is for Windows and macOS; on Linux the agent shares the screen itself")
}

// SPDX-License-Identifier: Apache-2.0

package core

import (
	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/desktop/x11"
	"github.com/aazam476/sentinel/internal/agent/policy"
)

// platformScreen is the desktop remote desktop shares on Linux: the
// active X11 session, or the display the local policy names.
func platformScreen(settings policy.DesktopSettings) desktop.Screen {
	return x11.NewScreen(command.Exec{}, settings.Display, settings.XAuthority)
}

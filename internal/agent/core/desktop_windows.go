// SPDX-License-Identifier: Apache-2.0

//go:build windows

package core

import (
	"context"
	"fmt"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/desktop/helper"
	"github.com/aazam476/sentinel/internal/agent/desktop/windesktop"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
)

// platformScreen is the console session's desktop, shared through the
// desktop helper the agent starts there (§9.3).
func platformScreen(policy.DesktopSettings) desktop.Screen {
	return windowsScreen{}
}

// platformNotifier is nil until Windows can ask (SEC-23) and show (SEC-24):
// a policy that wants consent refuses remote desktop.
func platformNotifier(policy.DesktopSettings) Notifier {
	return nil
}

// windowsScreen is the desktop of whoever is at the console, or the login
// screen.
type windowsScreen struct{}

// Availability says whether a session is attached to the console.
func (windowsScreen) Availability(context.Context) protocol.DesktopAvailability {
	if _, err := windesktop.ConsoleSession(); err != nil {
		return protocol.DesktopAvailability{Reason: "No session is at this machine's console; it may only be reached through Remote Desktop."}
	}
	return protocol.DesktopAvailability{Available: true}
}

// Open starts the desktop helper in the console session. It lives until
// the session closes it, not just while the request that started it runs.
func (windowsScreen) Open(context.Context) (desktop.Source, error) {
	session, err := windesktop.ConsoleSession()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", desktop.ErrUnavailable, err)
	}
	command, err := windesktop.HelperCommand(context.Background(), session)
	if err != nil {
		return nil, err
	}
	return helper.Launch(command)
}

// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/desktop/helper"
	"github.com/aazam476/sentinel/internal/agent/desktop/macdesktop"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
)

// platformScreen is the logged-in user's screen, shared through the
// desktop helper the agent starts in their session (§9.3).
func platformScreen(policy.DesktopSettings) desktop.Screen {
	return macScreen{}
}

// platformNotifier is nil until macOS can ask (SEC-23) and show (SEC-24):
// a policy that wants consent refuses remote desktop.
func platformNotifier(policy.DesktopSettings) Notifier {
	return nil
}

type macScreen struct{}

// Availability needs someone logged in and the person at the Mac to have
// allowed screen recording and control.
func (macScreen) Availability(context.Context) protocol.DesktopAvailability {
	if _, err := macdesktop.ConsoleUser(); err != nil {
		if errors.Is(err, macdesktop.ErrNoOneLoggedIn) {
			return protocol.DesktopAvailability{Reason: "No one is logged in at the Mac's screen."}
		}
		return protocol.DesktopAvailability{Reason: "The Mac's screen couldn't be found."}
	}
	if !macdesktop.Permitted() {
		return protocol.DesktopAvailability{Reason: "Desktop unavailable: permission needed on the Mac (Screen Recording and Accessibility)."}
	}
	return protocol.DesktopAvailability{Available: true}
}

// Open starts the desktop helper in the logged-in user's session.
func (macScreen) Open(context.Context) (desktop.Source, error) {
	uid, err := macdesktop.ConsoleUser()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", desktop.ErrUnavailable, err)
	}
	command, err := macdesktop.HelperCommand(context.Background(), uid)
	if err != nil {
		return nil, err
	}
	return helper.Launch(command)
}

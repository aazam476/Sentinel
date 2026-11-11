// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !windows && !darwin

package core

import (
	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/agent/policy"
)

// platformScreen is nil until this platform's capture is built (§9.3):
// the test pattern still works.
func platformScreen(policy.DesktopSettings) desktop.Screen {
	return nil
}

// platformNotifier is nil until this platform can ask and show (SEC-23,
// SEC-24): a policy that wants consent then refuses remote desktop.
func platformNotifier(policy.DesktopSettings) Notifier {
	return nil
}

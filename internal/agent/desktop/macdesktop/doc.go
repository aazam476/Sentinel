// SPDX-License-Identifier: Apache-2.0

// Package macdesktop shares a Mac's screen (§9.3). The agent runs as a
// root launch daemon, outside the logged-in user's graphical session, so
// it starts the desktop helper there (`launchctl asuser`) and the helper
// uses this package's Source.
//
// The screen is captured with the system's screencapture tool and input
// sent with CGEvent, called through purego: no cgo, like the rest of the
// agent. macOS asks the person at the Mac to allow Screen Recording and
// Accessibility for the agent; until they do, the machine says so.
//
//	console_darwin.go   who is at the screen, permissions, starting the helper
//	source_darwin.go    capture, input and the clipboard
//	keys_darwin.go      browser key codes to macOS key codes
package macdesktop

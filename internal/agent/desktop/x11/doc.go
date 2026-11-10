// SPDX-License-Identifier: Apache-2.0

// Package x11 shares an X11 desktop (§9.3): it finds the active graphical
// session, captures its screen with GetImage, and moves the pointer and
// presses keys with the XTEST extension, which works unattended.
//
// The agent runs as root, so it connects to the user's display itself,
// with the cookie from the session's Xauthority file; no helper process
// is needed, unlike on Windows and macOS.
//
//	session.go   finding the active X11 session (loginctl) and its cookie
//	xauth.go     reading Xauthority files (fuzzed)
//	source.go    the desktop.Source: capture and input
//	keys.go      browser key codes to X keycodes
package x11

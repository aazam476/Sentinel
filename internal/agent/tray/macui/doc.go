// SPDX-License-Identifier: Apache-2.0

// Package macui is the Sentinel tray's face on macOS: an item in the menu
// bar with a menu, notifications, and a dialog for remote desktop consent
// (SEC-23). The menu bar item is AppKit's NSStatusItem, called through
// purego's Objective-C runtime: no cgo (ADR 0027). Dialogs and
// notifications are shown by osascript, run as the signed-in user, so a
// question that goes away is closed by stopping it.
//
// AppKit must run on the process's main thread: `sentinel-agent tray`
// locks it at start-up and calls Run from main.
//
//	script.go     the osascript commands and their answers (every platform)
//	ui_darwin.go  the menu bar item
package macui

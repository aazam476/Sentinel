// SPDX-License-Identifier: Apache-2.0

// Package winui is the Sentinel tray's face on Windows: an icon in the
// notification area with a menu, balloon notifications, and a yes-or-no
// dialog for remote desktop consent (SEC-23). It calls user32, shell32
// and gdi32 directly through golang.org/x/sys/windows: no cgo (ADR 0027).
//
// The window that owns the icon is never shown; it only receives the
// icon's clicks. Everything that touches it runs on the one OS thread Run
// locks; Update and Notify post a message to that thread instead.
//
//	menu.go        the menu's entries and command IDs (every platform)
//	api_windows.go the Windows functions and structures used
//	ui_windows.go  the window, the icon and its menu
//	dialog_windows.go the consent dialog and the icon images
package winui

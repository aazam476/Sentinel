// SPDX-License-Identifier: Apache-2.0

// Package windesktop shares a Windows desktop (§9.3). The agent's service
// runs in session 0, which can't see anyone's screen, so it starts the
// desktop helper (internal/agent/desktop/helper) as SYSTEM in the active
// console session; the helper uses this package's Source.
//
// The helper follows the input desktop, so it also shows and drives UAC
// prompts and the login screen. Capture is GDI (BitBlt), input SendInput
// with scancodes (the machine's own layout applies), Ctrl+Alt+Del
// SendSAS. Everything is plain Win32 through golang.org/x/sys/windows: no
// cgo, like the rest of the agent.
//
//	launch_windows.go   finding the console session and starting the helper there
//	source_windows.go   capture, input and monitors
//	clipboard_windows.go  the text clipboard
//	keys_windows.go     browser key codes to scancodes
package windesktop

// SPDX-License-Identifier: Apache-2.0

//go:build windows

package windesktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// noSession is what WTSGetActiveConsoleSessionId returns with no session
// attached to the console.
const noSession = 0xFFFFFFFF

// ErrNoSession means no session is attached to the console (a server
// reached only by remote desktop, mid-switch).
var ErrNoSession = errors.New("no session is attached to the console")

// ConsoleSession is the session at the machine's screen, keyboard and
// mouse. The login screen is a session too, so it is there whenever the
// machine is on.
func ConsoleSession() (uint32, error) {
	session := windows.WTSGetActiveConsoleSessionId()
	if session == noSession {
		return 0, ErrNoSession
	}
	return session, nil
}

// HelperCommand is the desktop helper, `sentinel-agent desktop-helper`,
// to be started as SYSTEM in the console session: the agent's own token,
// moved to that session.
func HelperCommand(ctx context.Context, session uint32) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find the agent's program: %w", err)
	}
	token, err := sessionToken(session)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, executable, "desktop-helper") //nolint:gosec // G204: the agent's own program
	command.SysProcAttr = &syscall.SysProcAttr{
		Token:         syscall.Token(token),
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	return command, nil
}

// sessionToken duplicates the agent's (SYSTEM) token into session.
func sessionToken(session uint32) (windows.Token, error) {
	var own windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ALL_ACCESS, &own); err != nil {
		return 0, fmt.Errorf("open the agent's token: %w", err)
	}
	defer func() { _ = own.Close() }()
	var token windows.Token
	if err := windows.DuplicateTokenEx(own, windows.MAXIMUM_ALLOWED, nil, windows.SecurityImpersonation, windows.TokenPrimary, &token); err != nil {
		return 0, fmt.Errorf("copy the agent's token: %w", err)
	}
	if err := windows.SetTokenInformation(token, windows.TokenSessionId, (*byte)(unsafe.Pointer(&session)), uint32(unsafe.Sizeof(session))); err != nil { //nolint:gosec // G103: the session number, as the Win32 call takes it
		_ = token.Close()
		return 0, fmt.Errorf("move the token to session %d: %w", session, err)
	}
	return token, nil
}

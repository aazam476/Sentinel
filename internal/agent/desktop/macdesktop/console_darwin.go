// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package macdesktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// ErrNoOneLoggedIn means the Mac is at its login window.
var ErrNoOneLoggedIn = errors.New("no one is logged in at the Mac's screen")

// ConsoleUser is the user ID of whoever is logged in at the screen: the
// owner of /dev/console, which is root at the login window.
func ConsoleUser() (uint32, error) {
	info, err := os.Stat("/dev/console")
	if err != nil {
		return 0, fmt.Errorf("find who is at the screen: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid == 0 {
		return 0, ErrNoOneLoggedIn
	}
	return stat.Uid, nil
}

// HelperCommand is the desktop helper, started in the user's graphical
// session with launchctl.
func HelperCommand(ctx context.Context, uid uint32) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find the agent's program: %w", err)
	}
	//nolint:gosec // G204: launchctl and the agent's own program
	return exec.CommandContext(ctx, "/bin/launchctl", "asuser", strconv.FormatUint(uint64(uid), 10), executable, "desktop-helper"), nil
}

// Permitted says whether macOS lets the agent record the screen and send
// input, which only the person at the Mac can allow.
func Permitted() bool {
	g, err := loadGraphics()
	if err != nil {
		return false
	}
	return g.screenCaptureAllowed() && g.accessibilityAllowed()
}

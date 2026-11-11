// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// launchAgentPath is where macOS looks for launch agents for every user.
const launchAgentPath = "/Library/LaunchAgents/" + launchAgentLabel + ".plist"

// launchctlTimeout bounds starting or stopping the tray for the person at
// the screen now.
const launchctlTimeout = 10 * time.Second

// MachineAutostart starts the tray from a launch agent in every graphical
// session.
func MachineAutostart(executable string) Autostart {
	return macAutostart{executable: executable}
}

type macAutostart struct{ executable string }

// Install writes the launch agent, then starts it for whoever is at the
// screen, so they needn't sign in again.
func (a macAutostart) Install() error {
	if err := os.WriteFile(launchAgentPath, []byte(launchAgentPlist(a.executable)), 0o644); err != nil { //nolint:gosec // G306: launchd reads it as each user
		return fmt.Errorf("write the tray's launch agent: %w", err)
	}
	if uid, ok := consoleUser(); ok {
		_ = launchctl("bootstrap", "gui/"+uid, launchAgentPath) // it starts at their next sign-in otherwise
	}
	return nil
}

func (macAutostart) Uninstall() error {
	if uid, ok := consoleUser(); ok {
		_ = launchctl("bootout", "gui/"+uid+"/"+launchAgentLabel) // not running is fine
	}
	if err := os.Remove(launchAgentPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the tray's launch agent: %w", err)
	}
	return nil
}

func launchctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), launchctlTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "/bin/launchctl", args...).Run() //nolint:gosec,wrapcheck // G204: a fixed program; the caller ignores the error
}

// consoleUser is the uid of whoever is at the Mac's screen.
func consoleUser() (string, bool) {
	info, err := os.Stat("/dev/console")
	if err != nil {
		return "", false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid == 0 {
		return "", false
	}
	return strconv.FormatUint(uint64(stat.Uid), 10), true
}

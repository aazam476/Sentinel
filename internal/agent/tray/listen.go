// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// SocketPath is where the agent listens for trays on the operating system
// goos. On Windows, programData is %ProgramData%; it is ignored elsewhere.
func SocketPath(goos, programData string) string {
	switch goos {
	case "windows":
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return programData + `\Sentinel\tray\tray.sock`
	case "darwin":
		return "/var/run/sentinel-agent/tray.sock"
	default:
		return "/run/sentinel-agent/tray.sock"
	}
}

// Supported reports whether the tray runs on the operating system goos:
// Windows and macOS. On Linux the X11 prompts serve the person at the
// screen (ADR 0034), and no socket is opened.
func Supported(goos string) bool {
	return goos == "windows" || goos == "darwin"
}

// SocketPathForThisMachine is SocketPath for the running system.
func SocketPathForThisMachine() string {
	return SocketPath(runtime.GOOS, os.Getenv("ProgramData"))
}

// Dial connects a tray to the agent's socket.
func Dial(ctx context.Context, path string) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("reach the agent at %s: %w", path, err)
	}
	return conn, nil
}

// listenUnix opens a Unix socket at path.
func listenUnix(path string) (net.Listener, error) {
	var config net.ListenConfig
	listener, err := config.Listen(context.Background(), "unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on the tray socket: %w", err)
	}
	return listener, nil
}

// prepareSocketDir makes the socket's folder and deletes a socket a
// previous run left behind.
func prepareSocketDir(path string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), mode); err != nil {
		return err //nolint:wrapcheck // wrapped by Listen
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err //nolint:wrapcheck // wrapped by Listen
	}
	return nil
}

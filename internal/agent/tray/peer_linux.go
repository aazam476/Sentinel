// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerUID is the user on the other end of a Unix socket.
func peerUID(conn net.Conn) (int, error) {
	raw, err := rawConn(conn)
	if err != nil {
		return 0, err
	}
	var credentials *unix.Ucred
	var credentialsErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialsErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, fmt.Errorf("read the tray peer: %w", err)
	}
	if credentialsErr != nil {
		return 0, fmt.Errorf("read the tray peer: %w", credentialsErr)
	}
	return int(credentials.Uid), nil
}

// consoleUID: Linux has no single console user; the X11 prompts serve the
// person at the screen there (ADR 0034), so only root and the agent's
// user reach the socket.
func consoleUID() (int, bool) {
	return 0, false
}

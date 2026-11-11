// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// peerUID is the user on the other end of a Unix socket.
func peerUID(conn net.Conn) (int, error) {
	raw, err := rawConn(conn)
	if err != nil {
		return 0, err
	}
	var credentials *unix.Xucred
	var credentialsErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, credentialsErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return 0, fmt.Errorf("read the tray peer: %w", err)
	}
	if credentialsErr != nil {
		return 0, fmt.Errorf("read the tray peer: %w", credentialsErr)
	}
	return int(credentials.Uid), nil
}

// consoleUID is the user logged in at the Mac's screen: the owner of
// /dev/console.
func consoleUID() (int, bool) {
	info, err := os.Stat("/dev/console")
	if err != nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid == 0 {
		return 0, false
	}
	return int(stat.Uid), true
}

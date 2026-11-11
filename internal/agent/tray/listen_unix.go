// SPDX-License-Identifier: Apache-2.0

//go:build unix

package tray

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// Listen opens the tray socket at path. Anyone may connect to the file;
// each connection's user is then checked, and only root, the agent's own
// user and the user at the console are served (peer_*.go). It is a Unix
// socket, never a network port (SEC-26).
func Listen(path string) (net.Listener, error) {
	if err := prepareSocketDir(path, 0o755); err != nil {
		return nil, fmt.Errorf("prepare the tray socket: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G302: users must reach the socket inside; it holds nothing else
		return nil, fmt.Errorf("prepare the tray socket: %w", err)
	}
	listener, err := listenUnix(path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o666); err != nil { //nolint:gosec // G302: every user may connect; peers are checked one by one
		_ = listener.Close() // failing anyway
		return nil, fmt.Errorf("open the tray socket to users: %w", err)
	}
	return checkedListener{Listener: listener, check: checkPeer}, nil
}

// errPeerRefused means a connection came from someone the tray socket
// doesn't serve.
var errPeerRefused = errors.New("this user may not use the Sentinel tray socket")

// checkedListener drops connections from peers check refuses.
type checkedListener struct {
	net.Listener
	check func(net.Conn) error
}

// Accept returns the next connection check allows.
func (l checkedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err //nolint:wrapcheck // the listener's own error, as net.Listener returns it
		}
		if l.check(conn) == nil {
			return conn, nil
		}
		_ = conn.Close() // refused
	}
}

// checkPeer allows root, the agent's own user and the console's user.
func checkPeer(conn net.Conn) error {
	uid, err := peerUID(conn)
	if err != nil {
		return err
	}
	if uid == 0 || uid == os.Getuid() {
		return nil
	}
	if console, ok := consoleUID(); ok && console == uid {
		return nil
	}
	return errPeerRefused
}

// rawConn returns the connection's socket for getsockopt.
func rawConn(conn net.Conn) (syscall.RawConn, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, errPeerRefused
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("read the tray peer: %w", err)
	}
	return raw, nil
}

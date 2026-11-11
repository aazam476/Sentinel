// SPDX-License-Identifier: Apache-2.0

//go:build unix && !linux && !darwin

package tray

import "net"

// peerUID can't tell the user on this system, so it refuses everyone but
// lets checkPeer's caller see why.
func peerUID(net.Conn) (int, error) {
	return 0, errPeerRefused
}

func consoleUID() (int, bool) {
	return 0, false
}

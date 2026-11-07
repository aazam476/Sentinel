// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"context"
	"fmt"
	"net"
)

// Listen opens a TCP listener on address. Pass port 0 to let the operating
// system pick a free port; the listener's Addr method reports which one it
// chose.
//
// Which addresses are allowed is decided by the config package (plain HTTP
// only on loopback, SEC-10), not here.
//
// ctx only limits how long opening the socket may take; canceling it later
// does not close the listener.
func Listen(ctx context.Context, address string) (net.Listener, error) {
	var config net.ListenConfig
	listener, err := config.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", address, err)
	}
	return listener, nil
}

// SPDX-License-Identifier: Apache-2.0

package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// ErrNotLoopback is returned for a listen address on a non-loopback interface.
// It is never reassigned; it exists so callers can check for it with
// errors.Is.
var ErrNotLoopback = errors.New("only loopback addresses are allowed until TLS is supported")

// ValidateListenAddress checks that address is a host:port pair whose host is
// a loopback address ("127.0.0.1", "::1" or "localhost").
//
// An empty host (":7443") is rejected because it means "every interface".
func ValidateListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", address, err)
	}

	if host == "" {
		return fmt.Errorf("listen address %q: host is empty, which would listen on every interface; "+
			"use 127.0.0.1 until TLS is supported", address)
	}

	if strings.EqualFold(host, "localhost") {
		return nil
	}

	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("listen address %q: host must be an IP address or localhost: %w", address, err)
	}

	if !ip.IsLoopback() {
		return fmt.Errorf("listen address %q: %w", address, ErrNotLoopback)
	}

	return nil
}

// Listen validates address with ValidateListenAddress and opens a TCP
// listener on it. Pass port 0 to let the operating system pick a free port;
// the listener's Addr method reports which one it chose.
//
// ctx only limits how long opening the socket may take; canceling it later
// does not close the listener.
func Listen(ctx context.Context, address string) (net.Listener, error) {
	if err := ValidateListenAddress(address); err != nil {
		return nil, err
	}

	var config net.ListenConfig
	listener, err := config.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", address, err)
	}

	return listener, nil
}

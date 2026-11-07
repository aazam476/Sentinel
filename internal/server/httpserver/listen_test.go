// SPDX-License-Identifier: Apache-2.0

package httpserver_test

import (
	"net"
	"testing"

	"github.com/aazam476/sentinel/internal/server/httpserver"
)

func TestListen(t *testing.T) {
	t.Run("listens on a port chosen by the system", func(t *testing.T) {
		listener, err := httpserver.Listen(t.Context(), "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen returned %v", err)
		}
		defer closeListener(t, listener)

		address, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatalf("listener address is %T, want *net.TCPAddr", listener.Addr())
		}
		if address.Port == 0 {
			t.Errorf("listener address = %v, want a real port", address)
		}
	})

	t.Run("returns an error when the port is already in use", func(t *testing.T) {
		first, err := httpserver.Listen(t.Context(), "127.0.0.1:0")
		if err != nil {
			t.Fatalf("first Listen returned %v", err)
		}
		defer closeListener(t, first)

		second, err := httpserver.Listen(t.Context(), first.Addr().String())
		if err == nil {
			closeListener(t, second)
			t.Fatal("second Listen on the same port returned nil, want an error")
		}
	})

	t.Run("returns an error for a malformed address", func(t *testing.T) {
		if _, err := httpserver.Listen(t.Context(), "not an address"); err == nil {
			t.Fatal("Listen returned nil for a malformed address")
		}
	})
}

func closeListener(t *testing.T, listener net.Listener) {
	t.Helper()
	if err := listener.Close(); err != nil {
		t.Errorf("close listener: %v", err)
	}
}

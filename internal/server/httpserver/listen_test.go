// SPDX-License-Identifier: Apache-2.0

package httpserver_test

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/httpserver"
)

// SEC-10 allows plain HTTP only on localhost. Until TLS exists, every listen
// address must be loopback.
func TestSEC10_ValidateListenAddressAcceptsOnlyLoopback(t *testing.T) {
	tests := []struct {
		name          string
		address       string
		wantErr       bool
		wantNotLoopIP bool
	}{
		{name: "accepts the IPv4 loopback address", address: "127.0.0.1:7443"},
		{name: "accepts any address in the IPv4 loopback range", address: "127.0.0.2:7443"},
		{name: "accepts the IPv6 loopback address", address: "[::1]:7443"},
		{name: "accepts localhost", address: "localhost:7443"},
		{name: "accepts localhost in any letter case", address: "LocalHost:7443"},
		{name: "accepts port zero so the system can pick a port", address: "127.0.0.1:0"},
		{name: "rejects an empty host because it means every interface", address: ":7443", wantErr: true},
		{name: "rejects the IPv4 wildcard address", address: "0.0.0.0:7443", wantErr: true, wantNotLoopIP: true},
		{name: "rejects the IPv6 wildcard address", address: "[::]:7443", wantErr: true, wantNotLoopIP: true},
		{name: "rejects a LAN address", address: "192.168.1.5:7443", wantErr: true, wantNotLoopIP: true},
		{name: "rejects a hostname other than localhost", address: "example.com:7443", wantErr: true},
		{name: "rejects an address without a port", address: "127.0.0.1", wantErr: true},
		{name: "rejects an empty address", address: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := httpserver.ValidateListenAddress(test.address)

			if test.wantErr && err == nil {
				t.Fatalf("ValidateListenAddress(%q) returned nil, want an error", test.address)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("ValidateListenAddress(%q) returned %v, want nil", test.address, err)
			}
			if test.wantNotLoopIP && !errors.Is(err, httpserver.ErrNotLoopback) {
				t.Errorf("ValidateListenAddress(%q) error = %v, want ErrNotLoopback", test.address, err)
			}
		})
	}
}

func TestListen(t *testing.T) {
	t.Run("listens on a loopback address chosen by the system", func(t *testing.T) {
		listener, err := httpserver.Listen(t.Context(), "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen returned %v", err)
		}
		defer closeListener(t, listener)

		address, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			t.Fatalf("listener address is %T, want *net.TCPAddr", listener.Addr())
		}
		if !address.IP.IsLoopback() || address.Port == 0 {
			t.Errorf("listener address = %v, want a loopback address with a real port", address)
		}
	})

	t.Run("refuses a non-loopback address without opening a socket", func(t *testing.T) {
		listener, err := httpserver.Listen(t.Context(), "0.0.0.0:0")
		if !errors.Is(err, httpserver.ErrNotLoopback) {
			t.Fatalf("Listen error = %v, want ErrNotLoopback", err)
		}
		if listener != nil {
			t.Errorf("Listen returned a listener for a rejected address")
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
}

func FuzzValidateListenAddress(f *testing.F) {
	for _, seed := range []string{
		"127.0.0.1:7443", "[::1]:0", "localhost:80", ":7443", "0.0.0.0:7443",
		"[::ffff:127.0.0.1]:1", "[fe80::1%eth0]:7443", "127.0.0.1", "", "[]:1",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, address string) {
		if httpserver.ValidateListenAddress(address) != nil {
			return
		}

		host, _, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatalf("accepted %q, which has no valid host:port form: %v", address, err)
		}
		if strings.EqualFold(host, "localhost") {
			return
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			t.Fatalf("accepted %q, whose host is neither an IP address nor localhost", address)
		}
		if !ip.IsLoopback() {
			t.Fatalf("accepted %q, whose host is not a loopback address", address)
		}
	})
}

func closeListener(t *testing.T, listener net.Listener) {
	t.Helper()
	if err := listener.Close(); err != nil {
		t.Errorf("close listener: %v", err)
	}
}

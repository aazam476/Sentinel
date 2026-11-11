// SPDX-License-Identifier: Apache-2.0

//go:build unix

package tray

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckPeerRefusesWhatIsntAUnixSocket(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close(); _ = server.Close() }()
	if err := checkPeer(server); !errors.Is(err, errPeerRefused) {
		t.Errorf("checkPeer(pipe) = %v, want errPeerRefused", err)
	}
	if _, ok := consoleUID(); ok && runtime.GOOS == "linux" {
		t.Error("Linux reported a console user")
	}
}

func TestCheckedListenerDropsRefusedPeers(t *testing.T) {
	dir, err := os.MkdirTemp("", "peer")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "s.sock")
	inner, err := listenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	refused := true
	listener := checkedListener{Listener: inner, check: func(net.Conn) error {
		if refused {
			refused = false
			return errPeerRefused
		}
		return nil
	}}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
		close(accepted)
	}()
	first, err := Dial(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	if _, err := first.Read(make([]byte, 1)); err == nil {
		t.Error("a refused peer's connection stayed open")
	}
	second, err := Dial(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	if conn := <-accepted; conn == nil {
		t.Error("the allowed peer wasn't accepted")
	} else {
		_ = conn.Close()
	}
	_ = listener.Close()
	if _, err := listener.Accept(); err == nil {
		t.Error("Accept on a closed listener succeeded")
	}
}

// SPDX-License-Identifier: Apache-2.0

package faultnet_test

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/testutil/faultnet"
)

// echoServer answers each line with the same line.
func echoServer(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				lines := bufio.NewScanner(conn)
				for lines.Scan() {
					if _, err := conn.Write(append(lines.Bytes(), '\n')); err != nil {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func roundTrip(t *testing.T, conn net.Conn, text string) (string, error) {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte(text + "\n")); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	return line, err
}

func dial(t *testing.T, proxy *faultnet.Proxy) net.Conn {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", proxy.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestProxy(t *testing.T) {
	t.Run("forwards traffic both ways", func(t *testing.T) {
		proxy := faultnet.New(t, echoServer(t))
		if got, err := roundTrip(t, dial(t, proxy), "hello"); err != nil || got != "hello\n" {
			t.Errorf("got %q (%v)", got, err)
		}
	})

	t.Run("Drop cuts open connections", func(t *testing.T) {
		proxy := faultnet.New(t, echoServer(t))
		conn := dial(t, proxy)
		if _, err := roundTrip(t, conn, "before"); err != nil {
			t.Fatal(err)
		}
		proxy.Drop()
		if _, err := roundTrip(t, conn, "after"); err == nil {
			t.Error("the connection survived Drop")
		}
	})

	t.Run("Refuse turns new connections away until lifted", func(t *testing.T) {
		proxy := faultnet.New(t, echoServer(t))
		proxy.Refuse(true)
		if _, err := roundTrip(t, dial(t, proxy), "x"); err == nil {
			t.Error("a refused connection worked")
		}
		proxy.Refuse(false)
		if _, err := roundTrip(t, dial(t, proxy), "x"); err != nil {
			t.Errorf("err = %v after Refuse(false)", err)
		}
	})

	t.Run("Stall holds traffic back without closing, and a drop still works while stalled", func(t *testing.T) {
		proxy := faultnet.New(t, echoServer(t))
		conn := dial(t, proxy)
		if _, err := roundTrip(t, conn, "warm up"); err != nil {
			t.Fatal(err)
		}
		proxy.Stall(true)
		proxy.Stall(true) // twice is the same as once
		if _, err := roundTrip(t, conn, "held"); err == nil {
			t.Error("traffic flowed while stalled")
		}
		if proxy.Connections() != 1 {
			t.Errorf("%d connections, want the stalled one still open", proxy.Connections())
		}
		proxy.Drop()
		proxy.Stall(false)
		proxy.Stall(false)
	})

	t.Run("SetTarget sends new connections elsewhere, and a dead target closes them", func(t *testing.T) {
		proxy := faultnet.New(t, "127.0.0.1:1")
		if _, err := roundTrip(t, dial(t, proxy), "x"); err == nil {
			t.Error("a connection to a dead target worked")
		}
		proxy.SetTarget(echoServer(t))
		if _, err := roundTrip(t, dial(t, proxy), "x"); err != nil {
			t.Errorf("err = %v after SetTarget", err)
		}
	})
}

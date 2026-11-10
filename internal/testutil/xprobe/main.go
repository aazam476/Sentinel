// SPDX-License-Identifier: Apache-2.0

// Command xprobe is the end-to-end tests' eyes and hands on a virtual X
// display: "xprobe :N paint" fills a red square at (100, 100), 60 pixels
// wide, and "xprobe :N pointer" prints where the pointer is and which
// buttons are down, so a test can check remote desktop showed the screen
// and moved the pointer.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "xprobe:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 || !strings.HasPrefix(args[0], ":") {
		return errors.New("usage: xprobe :N paint|pointer")
	}
	netConn, err := (&net.Dialer{}).DialContext(context.Background(), "unix", "/tmp/.X11-unix/X"+strings.TrimPrefix(args[0], ":"))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	conn, err := xgb.NewConnNet(netConn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	switch args[1] {
	case "paint":
		return paint(conn, root)
	case "pointer":
		reply, err := xproto.QueryPointer(conn, root).Reply()
		if err != nil {
			return fmt.Errorf("query the pointer: %w", err)
		}
		fmt.Printf("%d %d %d\n", reply.RootX, reply.RootY, reply.Mask)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func paint(conn *xgb.Conn, root xproto.Window) error {
	gc, err := xproto.NewGcontextId(conn)
	if err != nil {
		return fmt.Errorf("paint: %w", err)
	}
	if err := xproto.CreateGCChecked(conn, gc, xproto.Drawable(root), xproto.GcForeground, []uint32{0xff0000}).Check(); err != nil {
		return fmt.Errorf("paint: %w", err)
	}
	square := []xproto.Rectangle{{X: 100, Y: 100, Width: 60, Height: 60}}
	if err := xproto.PolyFillRectangleChecked(conn, xproto.Drawable(root), gc, square).Check(); err != nil {
		return fmt.Errorf("paint: %w", err)
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0

package x11_test

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/aazam476/sentinel/internal/agent/desktop/x11"
)

// copies collects what a source sees copied.
func copies(t *testing.T, source *x11.Source) <-chan string {
	t.Helper()
	seen := make(chan string, 16)
	go source.WatchClipboard(t.Context(), func(text string) { seen <- text })
	return seen
}

// setUntilSeen sets text on one source until another sees it: the watcher
// starts in the background.
func setUntilSeen(t *testing.T, from *x11.Source, seen <-chan string, text string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if err := from.SetClipboard(text); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-seen:
			if got != text {
				t.Fatalf("saw %q, want %q", got, text)
			}
			return
		case <-time.After(100 * time.Millisecond):
		case <-deadline:
			t.Fatalf("%q was never seen", text)
		}
	}
}

func TestClipboard(t *testing.T) {
	display := startXvfb(t, false)
	agent, otherApp := open(t, display), open(t, display)

	t.Run("sees what another app copies", func(t *testing.T) {
		setUntilSeen(t, otherApp, copies(t, agent), "copied on the machine — ünïcode")
	})

	t.Run("gives apps that paste the text it was given", func(t *testing.T) {
		setUntilSeen(t, agent, copies(t, otherApp), "pasted from the browser")
	})

	t.Run("passes on long text, up to the limit", func(t *testing.T) {
		setUntilSeen(t, otherApp, copies(t, agent), strings.Repeat("0123456789", 6000))
	})
}

func TestClipboardForms(t *testing.T) {
	display := startXvfb(t, false)
	agent := open(t, display)
	if err := agent.SetClipboard("text"); err != nil {
		t.Fatal(err)
	}
	conn, root := observer(t, display)
	atom := func(name string) xproto.Atom {
		reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply() //nolint:gosec // G115: short test names
		if err != nil {
			t.Fatal(err)
		}
		return reply.Atom
	}
	window, err := xproto.NewWindowId(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err := xproto.CreateWindowChecked(conn, 0, window, root, 0, 0, 1, 1, 0, xproto.WindowClassInputOnly, 0, 0, nil).Check(); err != nil {
		t.Fatal(err)
	}
	ask := func(target string) []byte {
		t.Helper()
		property := atom("ANSWER")
		if err := xproto.ConvertSelectionChecked(conn, window, atom("CLIPBOARD"), atom(target), property, xproto.TimeCurrentTime).Check(); err != nil {
			t.Fatal(err)
		}
		for {
			event, xerr := conn.WaitForEvent()
			if event == nil && xerr == nil {
				t.Fatal("the connection closed")
			}
			if notify, ok := event.(xproto.SelectionNotifyEvent); ok {
				if notify.Property == 0 {
					return nil
				}
				reply, err := xproto.GetProperty(conn, true, window, property, xproto.GetPropertyTypeAny, 0, 1024).Reply()
				if err != nil {
					t.Fatal(err)
				}
				return reply.Value
			}
		}
	}

	targets := ask("TARGETS")
	var names []xproto.Atom
	for i := 0; i+4 <= len(targets); i += 4 {
		names = append(names, xproto.Atom(binary.LittleEndian.Uint32(targets[i:])))
	}
	if len(names) != 3 || names[1] != atom("UTF8_STRING") || names[2] != atom("STRING") {
		t.Errorf("targets = %v", names)
	}
	if got := string(ask("STRING")); got != "text" {
		t.Errorf("STRING = %q", got)
	}
	if got := ask("image/png"); got != nil {
		t.Errorf("image/png = %v, want no answer", got)
	}
	_ = xgb.Pad // xgb is the test's X client
}

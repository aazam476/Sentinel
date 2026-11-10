// SPDX-License-Identifier: Apache-2.0

package x11

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"

	"github.com/aazam476/sentinel/internal/protocol"
)

// clipboard shares the X CLIPBOARD selection, the one Ctrl+C and Ctrl+V
// use. X has no clipboard store: whoever copied last owns the selection
// and hands its text to each app that pastes. So to put text there, the
// agent becomes the owner and answers; to see what was copied, it asks
// XFIXES to tell it when the owner changes, then asks the new owner.
type clipboard struct {
	conn   *xgb.Conn
	window xproto.Window
	atoms  clipboardAtoms

	mutex   sync.Mutex
	text    string // what the agent offers while it owns the selection
	changed func(text string)
	done    chan struct{}
}

type clipboardAtoms struct {
	clipboard, utf8, text, targets, atom, property xproto.Atom
}

// openClipboard connects to the display with a window of its own, which
// owns the selection when the agent has text to offer.
func openClipboard(display Display) (*clipboard, error) {
	conn, err := dial(display)
	if err != nil {
		return nil, err
	}
	c := &clipboard{conn: conn, done: make(chan struct{})}
	if err := c.setUp(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("share the clipboard of X display %s: %w", display.Name(), err)
	}
	go c.serve()
	return c, nil
}

func (c *clipboard) setUp() error {
	if !hasExtension(c.conn, extensionXFIXES) {
		return errors.New("no XFIXES extension")
	}
	if _, err := xfixes.QueryVersion(c.conn, 5, 0).Reply(); err != nil {
		return fmt.Errorf("check XFIXES: %w", err)
	}
	for name, atom := range map[string]*xproto.Atom{
		"CLIPBOARD": &c.atoms.clipboard, "UTF8_STRING": &c.atoms.utf8, "STRING": &c.atoms.text,
		"TARGETS": &c.atoms.targets, "ATOM": &c.atoms.atom, "SENTINEL_CLIPBOARD": &c.atoms.property,
	} {
		reply, err := xproto.InternAtom(c.conn, false, uint16(len(name)), name).Reply() //nolint:gosec // G115: short names
		if err != nil {
			return fmt.Errorf("look up %s: %w", name, err)
		}
		*atom = reply.Atom
	}
	screen := xproto.Setup(c.conn).DefaultScreen(c.conn)
	var err error
	if c.window, err = xproto.NewWindowId(c.conn); err != nil {
		return fmt.Errorf("make a window: %w", err)
	}
	if err := xproto.CreateWindowChecked(c.conn, 0, c.window, screen.Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOnly, 0, 0, nil).Check(); err != nil {
		return fmt.Errorf("make a window: %w", err)
	}
	if err := xfixes.SelectSelectionInputChecked(c.conn, c.window, c.atoms.clipboard,
		xfixes.SelectionEventMaskSetSelectionOwner).Check(); err != nil {
		return fmt.Errorf("watch the clipboard: %w", err)
	}
	return nil
}

// set offers text as the clipboard's.
func (c *clipboard) set(text string) error {
	c.mutex.Lock()
	c.text = text
	c.mutex.Unlock()
	if err := xproto.SetSelectionOwnerChecked(c.conn, c.window, c.atoms.clipboard, xproto.TimeCurrentTime).Check(); err != nil {
		return fmt.Errorf("take the clipboard: %w", err)
	}
	return nil
}

// watch sets who hears what is copied on the machine; nil stops.
func (c *clipboard) watch(changed func(text string)) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.changed = changed
}

func (c *clipboard) close() {
	c.conn.Close()
	<-c.done
}

// serve handles the window's events until the connection closes.
func (c *clipboard) serve() {
	defer close(c.done)
	for {
		event, err := c.conn.WaitForEvent()
		if event == nil && err == nil {
			return
		}
		switch e := event.(type) {
		case xproto.SelectionRequestEvent:
			c.answer(e)
		case xfixes.SelectionNotifyEvent:
			if e.Owner != c.window && e.Owner != 0 {
				// Something else copied: ask it for the text, as UTF-8.
				_ = xproto.ConvertSelectionChecked(c.conn, c.window, c.atoms.clipboard, c.atoms.utf8, c.atoms.property, e.SelectionTimestamp).Check() // a refusal just means no text
			}
		case xproto.SelectionNotifyEvent:
			c.received(e)
		}
	}
}

// answer gives an app that pastes the agent's text, or the list of forms
// it comes in.
func (c *clipboard) answer(request xproto.SelectionRequestEvent) {
	property := request.Property
	if property == 0 {
		property = request.Target // an old client's request
	}
	c.mutex.Lock()
	text := c.text
	c.mutex.Unlock()
	var err error
	switch request.Target {
	case c.atoms.targets:
		targets := make([]byte, 0, 12)
		for _, atom := range []xproto.Atom{c.atoms.targets, c.atoms.utf8, c.atoms.text} {
			targets = binary.LittleEndian.AppendUint32(targets, uint32(atom)) // xgb speaks X in little-endian order
		}
		err = xproto.ChangePropertyChecked(c.conn, xproto.PropModeReplace, request.Requestor, property, c.atoms.atom, 32, 3, targets).Check()
	case c.atoms.utf8, c.atoms.text:
		err = xproto.ChangePropertyChecked(c.conn, xproto.PropModeReplace, request.Requestor, property, request.Target, 8,
			uint32(len(text)), []byte(text)).Check() //nolint:gosec // G115: at most MaxClipboardBytes
	default:
		property = 0 // a form the agent doesn't offer
	}
	if err != nil {
		property = 0
	}
	reply := xproto.SelectionNotifyEvent{
		Time: request.Time, Requestor: request.Requestor, Selection: request.Selection, Target: request.Target, Property: property,
	}
	_ = xproto.SendEventChecked(c.conn, false, request.Requestor, 0, string(reply.Bytes())).Check() // the app may be gone
}

// received reads the text another app gave in answer to the agent's ask.
func (c *clipboard) received(notify xproto.SelectionNotifyEvent) {
	if notify.Property == 0 {
		return // it had no text
	}
	reply, err := xproto.GetProperty(c.conn, true, c.window, notify.Property, xproto.GetPropertyTypeAny, 0,
		(protocol.MaxClipboardBytes+3)/4).Reply()
	if err != nil || reply.BytesAfter > 0 || (reply.Type != c.atoms.utf8 && reply.Type != c.atoms.text) {
		return // too big, or not text
	}
	c.mutex.Lock()
	changed := c.changed
	c.mutex.Unlock()
	if changed != nil {
		changed(string(reply.Value))
	}
}

// SetClipboard puts text on the display's clipboard (desktop.Clipboard).
func (s *Source) SetClipboard(text string) error {
	c, err := s.clipboardOnce()
	if err != nil {
		return err
	}
	return c.set(text)
}

// WatchClipboard calls changed with what apps on the display copy, until
// ctx ends.
func (s *Source) WatchClipboard(ctx context.Context, changed func(text string)) {
	c, err := s.clipboardOnce()
	if err != nil {
		return // no clipboard to share; the session goes on without
	}
	c.watch(changed)
	<-ctx.Done()
	c.watch(nil)
}

// clipboardOnce opens the display's clipboard on first use.
func (s *Source) clipboardOnce() (*clipboard, error) {
	s.clipboardMutex.Lock()
	defer s.clipboardMutex.Unlock()
	if s.clipboard == nil {
		c, err := openClipboard(s.display)
		if err != nil {
			return nil, err
		}
		s.clipboard = c
	}
	return s.clipboard, nil
}

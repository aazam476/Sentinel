// SPDX-License-Identifier: Apache-2.0

package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

// errGone means the helper stopped answering: it exited or the pipe broke.
var errGone = errors.New("the desktop helper has stopped")

// stopTimeout is how long a helper has to exit once its input closes.
const stopTimeout = 5 * time.Second

// Client drives a desktop helper. It is a desktop.Source with a clipboard
// and monitors, whatever the helper's own source supports.
type Client struct {
	in   io.WriteCloser
	stop func() error

	// requests go one at a time, each waiting for its reply.
	requests sync.Mutex
	replies  chan reply
	done     chan struct{}

	mutex   sync.Mutex
	name    string
	width   int
	height  int
	copied  func(text string)
	watched bool
}

type reply struct {
	ok      bool
	payload []byte
}

// NewClient talks to a helper that reads in and writes out; stop waits
// for it to end once in is closed.
func NewClient(in io.WriteCloser, out io.Reader, stop func() error) (*Client, error) {
	c := &Client{in: in, stop: stop, replies: make(chan reply, 1), done: make(chan struct{})}
	go c.read(bufio.NewReader(out))
	name, err := c.request(kindName, nil)
	if err != nil {
		_ = c.Close() // it never worked
		return nil, err
	}
	c.name = string(name)
	return c, nil
}

// Launch starts command, a desktop helper, and returns its client.
func Launch(command *exec.Cmd) (*Client, error) {
	in, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("start the desktop helper: %w", err)
	}
	out, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start the desktop helper: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start the desktop helper: %w", err)
	}
	stop := func() error {
		exited := make(chan error, 1)
		go func() { exited <- command.Wait() }()
		select {
		case err := <-exited:
			return err
		case <-time.After(stopTimeout):
			_ = command.Process.Kill() // it ignored its input closing
			return <-exited
		}
	}
	return NewClient(in, out, stop)
}

// read routes the helper's messages: replies to the waiting request,
// clipboard changes to the watcher.
func (c *Client) read(r *bufio.Reader) {
	defer close(c.done)
	for {
		kind, payload, err := readFrame(r)
		if err != nil {
			return
		}
		switch kind {
		case kindOK, kindFailed:
			c.replies <- reply{ok: kind == kindOK, payload: payload}
		case kindCopied:
			c.mutex.Lock()
			copied := c.copied
			c.mutex.Unlock()
			if copied != nil {
				copied(string(payload))
			}
		}
	}
}

// request sends one request and waits for its reply.
func (c *Client) request(kind byte, payload []byte) ([]byte, error) {
	c.requests.Lock()
	defer c.requests.Unlock()
	if err := writeFrame(c.in, kind, payload); err != nil {
		return nil, err
	}
	select {
	case r := <-c.replies:
		if !r.ok {
			return nil, fmt.Errorf("the desktop helper: %s", r.payload)
		}
		return r.payload, nil
	case <-c.done:
		return nil, errGone
	}
}

// Name is what the helper shares.
func (c *Client) Name() string { return c.name }

// Size is the helper's screen size; if it can't say, the last it said.
// The lock is taken only after the reply: the reader takes it too, to
// pass on a clipboard change that may come first.
func (c *Client) Size() (int, int) {
	payload, err := c.request(kindSize, nil)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err == nil {
		if width, height, err := decodeSize(payload); err == nil {
			c.width, c.height = width, height
		}
	}
	return c.width, c.height
}

// Capture fills frame with the whole screen.
func (c *Client) Capture(frame *image.RGBA) error {
	return c.CaptureRegion(frame, image.Point{})
}

// CaptureRegion fills frame with the part of the screen at origin.
func (c *Client) CaptureRegion(frame *image.RGBA, origin image.Point) error {
	bounds := frame.Bounds()
	pixels, err := c.request(kindCapture, encodeRegion(region{
		X: int32(origin.X), Y: int32(origin.Y), Width: uint32(bounds.Dx()), Height: uint32(bounds.Dy()), //nolint:gosec // G115: screen coordinates fit
	}))
	if err != nil {
		return err
	}
	if len(pixels) != len(frame.Pix) {
		return fmt.Errorf("%w: %d bytes of pixels for %v", ErrBadMessage, len(pixels), bounds)
	}
	copy(frame.Pix, pixels)
	return nil
}

// Input passes on a mouse or keyboard event.
func (c *Client) Input(event protocol.DesktopInputEvent) error {
	_, err := c.request(kindInput, encodeJSON(event))
	return err
}

// SetClipboard puts text on the machine's clipboard.
func (c *Client) SetClipboard(text string) error {
	_, err := c.request(kindSetClipboard, []byte(text))
	return err
}

// WatchClipboard calls changed with what the machine copies, until ctx
// ends.
func (c *Client) WatchClipboard(ctx context.Context, changed func(text string)) {
	c.mutex.Lock()
	c.copied = changed
	first := !c.watched
	c.watched = true
	c.mutex.Unlock()
	if first {
		if _, err := c.request(kindWatchClipboard, nil); err != nil {
			return
		}
	}
	select {
	case <-ctx.Done():
	case <-c.done:
	}
	c.mutex.Lock()
	c.copied = nil
	c.mutex.Unlock()
}

// Monitors lists the helper's monitors.
func (c *Client) Monitors() ([]protocol.MonitorInfo, error) {
	payload, err := c.request(kindMonitors, nil)
	if err != nil {
		return nil, err
	}
	var monitors []protocol.MonitorInfo
	if err := json.Unmarshal(payload, &monitors); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadMessage, err)
	}
	return monitors, nil
}

// Close ends the helper: it exits when its input closes.
func (c *Client) Close() error {
	_ = c.in.Close() // the helper reads EOF and exits
	err := c.stop()
	<-c.done
	if err != nil {
		return fmt.Errorf("stop the desktop helper: %w", err)
	}
	return nil
}

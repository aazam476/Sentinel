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
	"sync"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Serve answers the agent's requests on r and w with source, until r ends
// (the agent closed the session, or went away).
func Serve(ctx context.Context, source desktop.Source, r io.Reader, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := &server{source: source, out: w}
	var watching sync.WaitGroup
	defer watching.Wait()
	defer cancel() // runs first: the clipboard watch stops before Serve returns
	reader := bufio.NewReader(r)
	for {
		kind, payload, err := readFrame(reader)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if kind == kindWatchClipboard {
			watching.Go(func() { s.watchClipboard(ctx) })
			err = s.reply(nil, nil)
		} else {
			err = s.reply(s.handle(kind, payload))
		}
		if err != nil {
			return err
		}
	}
}

// server writes replies and clipboard changes, one message at a time.
type server struct {
	source desktop.Source
	mutex  sync.Mutex
	out    io.Writer
	frame  *image.RGBA
}

func (s *server) send(kind byte, payload []byte) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return writeFrame(s.out, kind, payload)
}

func (s *server) reply(payload []byte, err error) error {
	if err != nil {
		return s.send(kindFailed, []byte(err.Error()))
	}
	return s.send(kindOK, payload)
}

// handle answers one request.
func (s *server) handle(kind byte, payload []byte) ([]byte, error) {
	switch kind {
	case kindName:
		return []byte(s.source.Name()), nil
	case kindSize:
		width, height := s.source.Size()
		return encodeSize(width, height), nil
	case kindCapture:
		return s.capture(payload)
	case kindInput:
		var event protocol.DesktopInputEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadMessage, err)
		}
		return nil, s.source.Input(event)
	case kindSetClipboard:
		clipboard, ok := s.source.(desktop.Clipboard)
		if !ok {
			return nil, errors.New("this desktop has no clipboard to share")
		}
		return nil, clipboard.SetClipboard(string(payload))
	case kindMonitors:
		return s.monitors()
	default:
		return nil, fmt.Errorf("%w: kind %d", ErrBadMessage, kind)
	}
}

// capture captures a region and returns its pixels, RGBA, row by row.
func (s *server) capture(payload []byte) ([]byte, error) {
	r, err := decodeRegion(payload)
	if err != nil {
		return nil, err
	}
	bounds := image.Rect(0, 0, int(r.Width), int(r.Height))
	if s.frame == nil || s.frame.Bounds() != bounds {
		s.frame = image.NewRGBA(bounds)
	}
	origin := image.Pt(int(r.X), int(r.Y))
	if regional, ok := s.source.(desktop.RegionCapturer); ok {
		err = regional.CaptureRegion(s.frame, origin)
	} else if origin == (image.Point{}) {
		err = s.source.Capture(s.frame)
	} else {
		err = errors.New("this desktop captures only the whole screen")
	}
	if err != nil {
		return nil, err
	}
	return s.frame.Pix, nil
}

func (s *server) monitors() ([]byte, error) {
	source, ok := s.source.(desktop.Monitors)
	if !ok {
		return encodeJSON([]protocol.MonitorInfo{}), nil
	}
	monitors, err := source.Monitors()
	if err != nil {
		return nil, err
	}
	return encodeJSON(monitors), nil
}

// watchClipboard sends each change to the machine's clipboard.
func (s *server) watchClipboard(ctx context.Context) {
	clipboard, ok := s.source.(desktop.Clipboard)
	if !ok {
		return
	}
	clipboard.WatchClipboard(ctx, func(text string) {
		_ = s.send(kindCopied, []byte(text)) // the agent going away ends Serve too
	})
}

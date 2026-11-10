// SPDX-License-Identifier: Apache-2.0

package desktop_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/aazam476/sentinel/internal/agent/desktop"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// screen rebuilds what the browser would show from screen updates.
type screen struct {
	mutex  sync.Mutex
	image  *image.RGBA
	frames int
	tiles  int
}

func (s *screen) apply(t *testing.T, data []byte) {
	t.Helper()
	update, err := protocol.DecodeScreenUpdate(data)
	if err != nil {
		t.Errorf("bad update: %v", err)
		return
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	switch update.Kind {
	case protocol.FrameScreen:
		s.image = image.NewRGBA(image.Rect(0, 0, update.Width, update.Height))
	case protocol.FrameTile:
		tile, err := jpeg.Decode(bytes.NewReader(update.JPEG))
		if err != nil {
			t.Errorf("bad tile: %v", err)
			return
		}
		for y := range update.Height {
			for x := range update.Width {
				s.image.Set(update.X+x, update.Y+y, tile.At(x, y))
			}
		}
		s.tiles++
	case protocol.FrameEnd:
		s.frames++
	}
}

func (s *screen) at(x, y int) color.RGBA {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.image == nil {
		return color.RGBA{}
	}
	return s.image.RGBAAt(x, y)
}

func (s *screen) count() (int, int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.frames, s.tiles
}

func isMagenta(c color.RGBA) bool {
	return c.R > 200 && c.G < 60 && c.B > 200
}

// streamOutput is the stream to the server, as the browser would see it.
type streamOutput struct {
	mutex    sync.Mutex
	controls []protocol.DesktopControlMessage
	updates  chan []byte
	fail     bool
}

func newStreamOutput() *streamOutput {
	return &streamOutput{updates: make(chan []byte, 10_000)}
}

func (o *streamOutput) Send(_ context.Context, data []byte) error {
	if o.fail {
		return errors.New("the connection dropped")
	}
	switch data[0] {
	case protocol.DesktopFrame:
		o.updates <- data[1:]
	case protocol.DesktopControl:
		var message protocol.DesktopControlMessage
		_ = json.Unmarshal(data[1:], &message)
		o.mutex.Lock()
		o.controls = append(o.controls, message)
		o.mutex.Unlock()
	}
	return nil
}

func (o *streamOutput) control(kind string) (protocol.DesktopControlMessage, bool) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	for _, message := range o.controls {
		if message.Type == kind {
			return message, true
		}
	}
	return protocol.DesktopControlMessage{}, false
}

func control(message protocol.DesktopControlMessage) []byte {
	encoded, _ := json.Marshal(message)
	return append([]byte{protocol.DesktopControl}, encoded...)
}

// session runs a session on a test pattern, returning its inbound messages.
func session(t *testing.T, options desktop.Options, out desktop.Output) (chan []byte, chan error) {
	t.Helper()
	pattern := desktop.NewTestPattern(200, 150)
	inbound := make(chan []byte, 100)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	go func() { done <- desktop.NewSession(pattern, options).Run(ctx, inbound, out) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("the session ended with %v", err)
		}
	})
	return inbound, done
}

func TestFallbackOverTheStream(t *testing.T) {
	clock := fakeclock.New(time.Now())
	out := newStreamOutput()
	inbound, _ := session(t, desktop.Options{Clock: clock}, out)
	var view screen

	// Nothing is sent until the browser says where.
	clock.Advance(time.Second)
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopFallback})
	// nextFrame moves the clock one frame on, then takes in everything the
	// session sent, so the view is up to date.
	nextFrame := func() {
		t.Helper()
		inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopAck})
		clock.Advance(100 * time.Millisecond)
		for {
			select {
			case data := <-out.updates:
				view.apply(t, data)
			case <-time.After(50 * time.Millisecond):
				return
			}
		}
	}
	nextFrame()
	nextFrame()
	if _, tiles := view.count(); tiles < 4*3 {
		t.Errorf("the first frame had %d tiles, want all 12", tiles)
	}

	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopInput, Input: &protocol.DesktopInputEvent{Kind: protocol.InputMouseDown, X: 100, Y: 100}})
	for range 3 {
		nextFrame()
	}
	// Off the mark's centre, where the pointer is drawn.
	if got := view.at(108, 108); !isMagenta(got) {
		t.Errorf("the click left %v, want the magenta mark", got)
	}
	_, tilesBefore := view.count()
	nextFrame()
	if _, tiles := view.count(); tiles-tilesBefore >= 12 {
		t.Errorf("a frame resent every tile (%d) though only the moving square changed", tiles-tilesBefore)
	}

	// A keyframe request or a new quality resends everything.
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopQuality, Quality: 200})
	nextFrame() // the session takes the message in
	_, tilesBefore = view.count()
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopKeyframe})
	nextFrame()
	nextFrame()
	if _, tiles := view.count(); tiles-tilesBefore < 12 {
		t.Errorf("after a quality change %d tiles came, want 12", tiles-tilesBefore)
	}
}

func TestViewOnlyIgnoresInput(t *testing.T) {
	clock := fakeclock.New(time.Now())
	pattern := desktop.NewTestPattern(100, 100)
	out := newStreamOutput()
	inbound := make(chan []byte, 10)
	done := make(chan error, 1)
	go func() {
		done <- desktop.NewSession(pattern, desktop.Options{Clock: clock, ViewOnly: true}).Run(t.Context(), inbound, out)
	}()
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopInput, Input: &protocol.DesktopInputEvent{Kind: protocol.InputMouseDown, X: 50, Y: 50}})
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopFallback})
	inbound <- []byte("x")  // not a control message
	inbound <- []byte("c{") // not JSON
	close(inbound)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	frame := image.NewRGBA(image.Rect(0, 0, 100, 100))
	if err := pattern.Capture(frame); err != nil {
		t.Fatal(err)
	}
	if isMagenta(frame.RGBAAt(50, 50)) {
		t.Error("a view-only session applied a click")
	}
}

func TestStreamFailureEndsTheSession(t *testing.T) {
	clock := fakeclock.New(time.Now())
	out := newStreamOutput()
	out.fail = true
	inbound, done := session(t, desktop.Options{Clock: clock}, out)
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopFallback})
	deadline := time.After(5 * time.Second)
	for {
		clock.Advance(100 * time.Millisecond)
		select {
		case err := <-done:
			if err == nil {
				t.Error("the session ended without saying why")
			}
			done <- nil // for the cleanup
			return
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("the session kept going with the stream gone")
		}
	}
}

// TestWebRTC connects a WebRTC peer standing in for the browser, over
// loopback, and checks frames and input travel on the data channels.
func TestWebRTC(t *testing.T) {
	clock := fakeclock.New(time.Now())
	out := newStreamOutput()
	inbound, _ := session(t, desktop.Options{Clock: clock}, out)

	browser, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	frames, err := browser.CreateDataChannel("frames", nil)
	if err != nil {
		t.Fatal(err)
	}
	input, err := browser.CreateDataChannel("input", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := browser.CreateDataChannel("unexpected", nil); err != nil {
		t.Fatal(err)
	}
	var view screen
	received := make(chan struct{}, 1000)
	frames.OnMessage(func(message webrtc.DataChannelMessage) {
		view.apply(t, message.Data)
		received <- struct{}{}
	})
	inputOpen := make(chan struct{})
	input.OnOpen(func() { close(inputOpen) })
	browser.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		init := candidate.ToJSON()
		inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopCandidate, Candidate: &protocol.ICECandidate{
			Candidate: init.Candidate, SDPMid: init.SDPMid, SDPMLineIndex: init.SDPMLineIndex,
		}})
	})
	offer, err := browser.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := browser.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopOffer, SDP: offer.SDP})

	deadline := time.After(15 * time.Second)
	answered := false
	for frames, _ := view.count(); frames < 3; frames, _ = view.count() {
		if message, found := out.control(protocol.DesktopAnswer); found && !answered {
			answered = true
			if err := browser.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: message.SDP}); err != nil {
				t.Fatal(err)
			}
			out.mutex.Lock()
			candidates := out.controls
			out.mutex.Unlock()
			for _, candidate := range candidates {
				if candidate.Type == protocol.DesktopCandidate {
					_ = browser.AddICECandidate(webrtc.ICECandidateInit{Candidate: candidate.Candidate.Candidate, SDPMid: candidate.Candidate.SDPMid, SDPMLineIndex: candidate.Candidate.SDPMLineIndex})
				}
			}
		}
		clock.Advance(100 * time.Millisecond)
		select {
		case <-received:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatal("no frames arrived over WebRTC")
		}
	}
	<-inputOpen
	click, _ := json.Marshal(protocol.DesktopControlMessage{Type: protocol.DesktopInput, Input: &protocol.DesktopInputEvent{Kind: protocol.InputMouseDown, X: 30, Y: 120}})
	if err := input.Send(click); err != nil {
		t.Fatal(err)
	}
	for !isMagenta(view.at(38, 128)) {
		clock.Advance(100 * time.Millisecond)
		select {
		case <-received:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatal("the click over WebRTC never showed")
		}
	}
	if len(out.updates) != 0 {
		t.Error("frames went over the stream though WebRTC was open")
	}
}

func TestBadOffer(t *testing.T) {
	clock := fakeclock.New(time.Now())
	out := newStreamOutput()
	inbound, _ := session(t, desktop.Options{Clock: clock}, out)
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopOffer, SDP: "not an offer"})
	inbound <- control(protocol.DesktopControlMessage{Type: protocol.DesktopCandidate, Candidate: &protocol.ICECandidate{Candidate: "x"}})
	deadline := time.After(5 * time.Second)
	for {
		if message, found := out.control(protocol.DesktopError); found {
			if message.Message == "" {
				t.Error("the error says nothing")
			}
			return
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("a bad offer wasn't reported")
		}
	}
}

func TestTestPattern(t *testing.T) {
	pattern := desktop.NewTestPattern(64, 48)
	if pattern.Name() != "test pattern" || pattern.Close() != nil {
		t.Error("unexpected name or close")
	}
	if width, height := pattern.Size(); width != 64 || height != 48 {
		t.Errorf("size %dx%d", width, height)
	}
	for _, kind := range []string{protocol.InputMouseMove, protocol.InputKeyDown, protocol.InputWheel} {
		if err := pattern.Input(protocol.DesktopInputEvent{Kind: kind, X: 10, Y: 10}); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	frame := image.NewRGBA(image.Rect(0, 0, 64, 48))
	if err := pattern.Capture(frame); err != nil {
		t.Fatal(err)
	}
}

func TestProvider(t *testing.T) {
	if availability := (desktop.Provider{TestPattern: true}).Availability(t.Context()); !availability.Available {
		t.Errorf("the test pattern isn't available: %+v", availability)
	}
	if source, err := (desktop.Provider{TestPattern: true}).Open(t.Context()); err != nil || source.Name() != "test pattern" {
		t.Errorf("got %v, %v", source, err)
	}
	if availability := (desktop.Provider{}).Availability(t.Context()); availability.Available || availability.Reason == "" {
		t.Errorf("a desktop is available with no display: %+v", availability)
	}
	if _, err := (desktop.Provider{}).Open(t.Context()); !errors.Is(err, desktop.ErrUnavailable) {
		t.Errorf("got %v", err)
	}
	screen := fakeScreen{availability: protocol.DesktopAvailability{Reason: "Nobody is here."}}
	if got := (desktop.Provider{Screen: screen}).Availability(t.Context()); got != screen.availability {
		t.Errorf("availability = %+v, want the screen's", got)
	}
	if source, err := (desktop.Provider{Screen: screen}).Open(t.Context()); err != nil || source.Name() != "screen" {
		t.Errorf("got %v, %v, want the screen", source, err)
	}
	if source, _ := (desktop.Provider{TestPattern: true, Screen: screen}).Open(t.Context()); source.Name() != "test pattern" {
		t.Errorf("the policy's test pattern gave way to %s", source.Name())
	}
}

// fakeScreen is a platform's desktop.
type fakeScreen struct {
	availability protocol.DesktopAvailability
}

func (s fakeScreen) Availability(context.Context) protocol.DesktopAvailability { return s.availability }

func (fakeScreen) Open(context.Context) (desktop.Source, error) { return namedSource{}, nil }

type namedSource struct{ desktop.Source }

func (namedSource) Name() string { return "screen" }

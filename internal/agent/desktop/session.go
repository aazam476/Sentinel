// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// frameInterval paces capture: about 15 frames a second.
const frameInterval = 66 * time.Millisecond

// maxBuffered is how much may wait in the data channel before frames are
// skipped, so a slow link shows a recent screen rather than falling behind.
const maxBuffered = 4 << 20

// Data channel labels, as the browser opens them.
const (
	framesChannel = "frames"
	inputChannel  = "input"
)

// Output sends a session's messages to the server, on its stream.
type Output interface {
	Send(ctx context.Context, data []byte) error
}

// Clock tells the time and makes timers.
type Clock interface {
	Now() time.Time
	NewTimer(duration time.Duration) clock.Timer
}

// Options configures a session.
type Options struct {
	ViewOnly   bool
	ICEServers []protocol.ICEServer
	Clock      Clock
	Logger     *slog.Logger
}

// Session shares a source with one browser.
type Session struct {
	source  Source
	options Options
	encoder *encoder
	frame   *image.RGBA

	// events runs work on the session's own goroutine, which alone touches
	// the encoder: the data channels call back on others.
	events chan func()

	peerMutex sync.Mutex
	peer      *webrtc.PeerConnection
	frames    *webrtc.DataChannel
	// framesOpen says frames go over WebRTC; fallback, over the stream.
	framesOpen atomic.Bool
	fallback   bool
	// awaitingAck holds back frames over the stream until the browser has
	// drawn the last one.
	awaitingAck bool
}

// NewSession returns a session sharing source.
func NewSession(source Source, options Options) *Session {
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	return &Session{source: source, options: options, encoder: newEncoder(), events: make(chan func(), 16)}
}

// Run serves the session until ctx ends or the stream closes (inbound is
// closed), reading the browser's messages from inbound and sending to out.
func (s *Session) Run(ctx context.Context, inbound <-chan []byte, out Output) error {
	defer s.closePeer()
	timer := s.options.Clock.NewTimer(frameInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case data, open := <-inbound:
			if !open {
				return nil
			}
			s.handle(ctx, data, out)
		case work := <-s.events:
			work()
		case <-timer.C():
			if err := s.sendFrame(ctx, out); err != nil {
				return err
			}
			timer.Reset(frameInterval)
		}
	}
}

// handle reads one message from the browser.
func (s *Session) handle(ctx context.Context, data []byte, out Output) {
	if len(data) < 2 || data[0] != protocol.DesktopControl {
		return
	}
	var message protocol.DesktopControlMessage
	if err := json.Unmarshal(data[1:], &message); err != nil {
		return
	}
	switch message.Type {
	case protocol.DesktopOffer:
		if err := s.answer(ctx, message.SDP, out); err != nil {
			s.options.Logger.Warn("couldn't answer the browser's WebRTC offer; frames go through the server", "error", err)
			sendControl(ctx, out, protocol.DesktopControlMessage{Type: protocol.DesktopError, Message: err.Error()})
		}
	case protocol.DesktopCandidate:
		s.addCandidate(message.Candidate)
	case protocol.DesktopFallback:
		s.fallback = true
		s.encoder.full = true
	case protocol.DesktopInput:
		s.input(message.Input)
	case protocol.DesktopQuality:
		s.encoder.setQuality(message.Quality)
	case protocol.DesktopKeyframe:
		s.encoder.full = true
	case protocol.DesktopAck:
		s.awaitingAck = false
	}
}

func (s *Session) input(event *protocol.DesktopInputEvent) {
	if event == nil || s.options.ViewOnly {
		return
	}
	if err := s.source.Input(*event); err != nil {
		s.options.Logger.Debug("couldn't apply an input event", "kind", event.Kind, "error", err)
	}
}

// sendFrame captures the screen and sends what changed, over WebRTC when
// its channel is open, else over the stream once the browser asked for
// that. Before either, or while the channel is full, it waits.
func (s *Session) sendFrame(ctx context.Context, out Output) error {
	webRTC := s.framesOpen.Load() && !s.fallback
	if !webRTC && !s.fallback {
		return nil
	}
	if (webRTC && s.buffered() > maxBuffered) || (!webRTC && s.awaitingAck) {
		return nil
	}
	width, height := s.source.Size()
	if s.frame == nil || s.frame.Bounds().Dx() != width || s.frame.Bounds().Dy() != height {
		s.frame = image.NewRGBA(image.Rect(0, 0, width, height))
	}
	if err := s.source.Capture(s.frame); err != nil {
		return fmt.Errorf("capture the screen: %w", err)
	}
	updates, err := s.encoder.encode(s.frame)
	if err != nil {
		return err
	}
	for _, update := range updates {
		if webRTC {
			if err := s.sendOnChannel(update); err != nil {
				// The browser closed the channel; its WebSocket says
				// whether the session goes on.
				s.options.Logger.Debug("couldn't send on the frames channel", "error", err)
				s.framesOpen.Store(false)
				return nil
			}
		} else if err := out.Send(ctx, append([]byte{protocol.DesktopFrame}, update...)); err != nil {
			return fmt.Errorf("send the screen: %w", err)
		}
	}
	s.awaitingAck = !webRTC && len(updates) > 0
	return nil
}

func sendControl(ctx context.Context, out Output, message protocol.DesktopControlMessage) {
	encoded, _ := json.Marshal(message) // a struct of strings and numbers always encodes
	_ = out.Send(ctx, append([]byte{protocol.DesktopControl}, encoded...))
}

// answer sets up the WebRTC connection the browser offered and sends the
// answer; ICE candidates follow as they are found.
func (s *Session) answer(ctx context.Context, offer string, out Output) error {
	// Browsers hide their local addresses behind mDNS names
	// (<uuid>.local), so the agent looks those up, but announces none of
	// its own: it has nothing to hide from a web page. Where multicast is
	// blocked, the lookup fails and frames go through the server.
	// https://datatracker.ietf.org/doc/html/draft-ietf-mmusic-mdns-ice-candidates
	settings := webrtc.SettingEngine{}
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryOnly)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	peer, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: iceServers(s.options.ICEServers)})
	if err != nil {
		return fmt.Errorf("start WebRTC: %w", err)
	}
	s.peerMutex.Lock()
	if s.peer != nil {
		_ = s.peer.Close()
	}
	s.peer = peer
	s.peerMutex.Unlock()
	peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		init := candidate.ToJSON()
		sendControl(ctx, out, protocol.DesktopControlMessage{Type: protocol.DesktopCandidate, Candidate: &protocol.ICECandidate{
			Candidate: init.Candidate, SDPMid: init.SDPMid, SDPMLineIndex: init.SDPMLineIndex,
		}})
	})
	peer.OnDataChannel(s.acceptChannel)
	peer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		s.options.Logger.Info("remote desktop WebRTC connection", "state", state.String())
	})
	if err := peer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return fmt.Errorf("read the offer: %w", err)
	}
	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("make an answer: %w", err)
	}
	if err := peer.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("make an answer: %w", err)
	}
	sendControl(ctx, out, protocol.DesktopControlMessage{Type: protocol.DesktopAnswer, SDP: answer.SDP})
	return nil
}

// acceptChannel takes the browser's data channels: frames goes out, input
// comes in.
func (s *Session) acceptChannel(channel *webrtc.DataChannel) {
	switch channel.Label() {
	case framesChannel:
		channel.OnOpen(func() {
			s.peerMutex.Lock()
			s.frames = channel
			s.peerMutex.Unlock()
			s.framesOpen.Store(true)
			s.run(func() { s.encoder.full = true })
		})
		channel.OnClose(func() { s.framesOpen.Store(false) })
	case inputChannel:
		channel.OnMessage(func(message webrtc.DataChannelMessage) {
			var control protocol.DesktopControlMessage
			if json.Unmarshal(message.Data, &control) == nil && control.Type == protocol.DesktopInput {
				s.run(func() { s.input(control.Input) })
			}
		})
	default:
		_ = channel.Close()
	}
}

// run queues work for the session's goroutine, dropping it if the session
// is too busy: input and refreshes are worth less than staying responsive.
func (s *Session) run(work func()) {
	select {
	case s.events <- work:
	default:
	}
}

func (s *Session) addCandidate(candidate *protocol.ICECandidate) {
	s.peerMutex.Lock()
	peer := s.peer
	s.peerMutex.Unlock()
	if peer == nil || candidate == nil {
		return
	}
	_ = peer.AddICECandidate(webrtc.ICECandidateInit{ // a bad candidate only means one path fewer
		Candidate: candidate.Candidate, SDPMid: candidate.SDPMid, SDPMLineIndex: candidate.SDPMLineIndex,
	})
}

func (s *Session) buffered() uint64 {
	s.peerMutex.Lock()
	defer s.peerMutex.Unlock()
	if s.frames == nil {
		return 0
	}
	return s.frames.BufferedAmount()
}

func (s *Session) sendOnChannel(update []byte) error {
	s.peerMutex.Lock()
	channel := s.frames
	s.peerMutex.Unlock()
	if channel == nil {
		return errors.New("the frames channel isn't open")
	}
	return channel.Send(update)
}

func (s *Session) closePeer() {
	s.peerMutex.Lock()
	defer s.peerMutex.Unlock()
	if s.peer != nil {
		_ = s.peer.Close() // the session is over either way
	}
}

func iceServers(servers []protocol.ICEServer) []webrtc.ICEServer {
	result := make([]webrtc.ICEServer, 0, len(servers))
	for _, server := range servers {
		result = append(result, webrtc.ICEServer{URLs: server.URLs, Username: server.Username, Credential: server.Credential})
	}
	return result
}

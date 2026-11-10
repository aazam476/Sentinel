// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"errors"
)

// ActionDesktopSessionStart starts a remote desktop session (§9.3) on a
// stream. The agent answers once the session is ready; after that the
// stream carries DesktopMessage values in both directions: control
// messages (WebRTC signaling, input, settings) and, when the browser can't
// reach the agent over WebRTC, the screen itself.
const ActionDesktopSessionStart = "desktop.session.start"

// ActionDesktopAvailability is the event an agent sends when whether its
// desktop can be shared changes: someone logged in or out. Its payload is
// a DesktopAvailability.
const ActionDesktopAvailability = "desktop.availability"

// DesktopStartPayload asks for a session.
type DesktopStartPayload struct {
	StreamID StreamID `json:"streamId"`
	// ViewOnly ignores the browser's mouse and keyboard.
	ViewOnly bool `json:"viewOnly,omitempty"`
	// ICEServers are the STUN and TURN servers the agent may use.
	ICEServers []ICEServer `json:"iceServers,omitempty"`
	// User is who asks, for the machine's consent prompt (SEC-23) and
	// session indicator (SEC-24).
	User string `json:"user,omitempty"`
}

// ICEServer is a STUN or TURN server for WebRTC.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// DesktopStartedPayload says the session is ready.
type DesktopStartedPayload struct {
	// Source names what is shared, e.g. "test pattern" or "X11 :0".
	Source string `json:"source"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// A desktop stream's data starts with a byte saying what follows.
const (
	// DesktopControl is followed by a JSON DesktopControlMessage.
	DesktopControl byte = 'c'
	// DesktopFrame is followed by an encoded screen update (EncodeTile...),
	// sent on the stream only when WebRTC isn't used.
	DesktopFrame byte = 'f'
)

// Desktop control message types.
const (
	// From the browser: a WebRTC offer, an ICE candidate, a request to send
	// the screen over the stream instead, input, settings, a full frame.
	DesktopOffer     = "offer"
	DesktopCandidate = "candidate"
	DesktopFallback  = "fallback"
	DesktopInput     = "input"
	DesktopQuality   = "quality"
	DesktopKeyframe  = "keyframe"
	// DesktopAck says the browser has drawn the last frame sent over the
	// stream; the agent sends the next only then, so a slow link isn't
	// flooded.
	DesktopAck = "ack"
	// From the agent: the answer, its ICE candidates, problems.
	DesktopAnswer = "answer"
	DesktopError  = "error"
)

// DesktopControlMessage is a control message on a desktop stream, or on
// the session's "input" data channel (input only).
type DesktopControlMessage struct {
	Type string `json:"type"`
	// SDP is an offer or answer.
	SDP string `json:"sdp,omitempty"`
	// Candidate is an ICE candidate as the browser's RTCIceCandidateInit.
	Candidate *ICECandidate `json:"candidate,omitempty"`
	// Input is a mouse or keyboard event.
	Input *DesktopInputEvent `json:"input,omitempty"`
	// Quality is the JPEG quality, 10 to 95.
	Quality int `json:"quality,omitempty"`
	// Message says what went wrong, for errors.
	Message string `json:"message,omitempty"`
}

// ICECandidate is an ICE candidate in the browser's shape.
type ICECandidate struct {
	Candidate     string  `json:"candidate"`
	SDPMid        *string `json:"sdpMid,omitempty"`
	SDPMLineIndex *uint16 `json:"sdpMLineIndex,omitempty"`
}

// Desktop input event kinds.
const (
	InputMouseMove  = "mousemove"
	InputMouseDown  = "mousedown"
	InputMouseUp    = "mouseup"
	InputWheel      = "wheel"
	InputKeyDown    = "keydown"
	InputKeyUp      = "keyup"
	InputCtrlAltDel = "ctrlaltdel"
)

// DesktopInputEvent is one mouse or keyboard event, in screen pixels.
type DesktopInputEvent struct {
	Kind string `json:"kind"`
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
	// Button is 0 (left), 1 (middle) or 2 (right).
	Button int `json:"button,omitempty"`
	// DeltaX and DeltaY are wheel steps.
	DeltaX int `json:"deltaX,omitempty"`
	DeltaY int `json:"deltaY,omitempty"`
	// Code is the key's KeyboardEvent.code ("KeyA", "Enter"), the same on
	// every keyboard layout; Key is what it typed, for text.
	Code string `json:"code,omitempty"`
	Key  string `json:"key,omitempty"`
}

// Screen update kinds, the first byte of an encoded update.
const (
	// FrameScreen gives the screen's size: u16 width, u16 height. It comes
	// first, and whenever the size changes.
	FrameScreen byte = 1
	// FrameTile is a rectangle of the screen: u16 x, y, width, height,
	// then a JPEG image of that size.
	FrameTile byte = 2
	// FrameEnd marks the end of one screen's worth of tiles, so the
	// browser can show them together.
	FrameEnd byte = 3
)

// ErrBadFrame means a screen update can't be read.
var ErrBadFrame = errors.New("malformed desktop frame")

// EncodeScreen encodes a FrameScreen update.
func EncodeScreen(width, height int) []byte {
	out := []byte{FrameScreen}
	out = binary.BigEndian.AppendUint16(out, uint16(width))   //nolint:gosec // G115: screens are under 65,536 pixels wide
	return binary.BigEndian.AppendUint16(out, uint16(height)) //nolint:gosec // G115: and high
}

// EncodeTile encodes a FrameTile update.
func EncodeTile(x, y, width, height int, jpeg []byte) []byte {
	out := make([]byte, 0, 9+len(jpeg))
	out = append(out, FrameTile)
	for _, value := range []int{x, y, width, height} {
		out = binary.BigEndian.AppendUint16(out, uint16(value)) //nolint:gosec // G115: screen coordinates fit
	}
	return append(out, jpeg...)
}

// EncodeEnd encodes a FrameEnd update.
func EncodeEnd() []byte {
	return []byte{FrameEnd}
}

// ScreenUpdate is a decoded screen update.
type ScreenUpdate struct {
	Kind                byte
	X, Y, Width, Height int
	JPEG                []byte
}

// DecodeScreenUpdate reads an encoded update.
func DecodeScreenUpdate(data []byte) (ScreenUpdate, error) {
	if len(data) == 0 {
		return ScreenUpdate{}, ErrBadFrame
	}
	update := ScreenUpdate{Kind: data[0]}
	switch update.Kind {
	case FrameScreen:
		if len(data) != 5 {
			return ScreenUpdate{}, ErrBadFrame
		}
		update.Width, update.Height = int(binary.BigEndian.Uint16(data[1:])), int(binary.BigEndian.Uint16(data[3:]))
	case FrameTile:
		if len(data) < 9 {
			return ScreenUpdate{}, ErrBadFrame
		}
		update.X, update.Y = int(binary.BigEndian.Uint16(data[1:])), int(binary.BigEndian.Uint16(data[3:]))
		update.Width, update.Height = int(binary.BigEndian.Uint16(data[5:])), int(binary.BigEndian.Uint16(data[7:]))
		update.JPEG = data[9:]
	case FrameEnd:
		if len(data) != 1 {
			return ScreenUpdate{}, ErrBadFrame
		}
	default:
		return ScreenUpdate{}, ErrBadFrame
	}
	return update, nil
}

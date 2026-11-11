// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

// Message types. The agent sends status, ask and asked; the tray sends
// answer, end and pause.
const (
	// TypeStatus: the agent's state, sent when a tray connects and on
	// every change.
	TypeStatus = "status"
	// TypeAsk: allow a remote desktop session? (SEC-23)
	TypeAsk = "ask"
	// TypeAsked: the question with this ID was answered elsewhere, or
	// timed out; take it off the screen.
	TypeAsked = "asked"
	// TypeAnswer: the person's answer to an ask.
	TypeAnswer = "answer"
	// TypeEnd: end the remote session with this ID.
	TypeEnd = "end"
	// TypePause: pause or resume remote access.
	TypePause = "pause"
)

// Session kinds.
const (
	KindTerminal = "terminal"
	KindDesktop  = "desktop"
)

// Limits on what a message may hold. The agent's messages are small; the
// caps keep a confused or hostile peer from making the other side buffer
// without end.
const (
	maxMessage  = 64 << 10
	maxSessions = 64
	maxText     = 512
)

// ErrBadMessage means a message can't be read.
var ErrBadMessage = errors.New("malformed tray message")

// Message is one line on the tray socket.
type Message struct {
	Type   string  `json:"type"`
	Status *Status `json:"status,omitempty"`
	// ID names the question for ask, asked and answer, and the session
	// for end.
	ID string `json:"id,omitempty"`
	// Question is the ask's text.
	Question string `json:"question,omitempty"`
	// Allow is the answer; Paused is pause's new state.
	Allow  bool `json:"allow,omitempty"`
	Paused bool `json:"paused,omitempty"`
}

// Status is what the tray shows.
type Status struct {
	// Connected is whether the agent is connected to its server.
	Connected bool   `json:"connected"`
	Server    string `json:"server"`
	Version   string `json:"version"`
	// Paused: the person at the machine paused remote access.
	Paused bool `json:"paused"`
	// ConsentRequired: remote desktop asks the person at the machine
	// first (SEC-23).
	ConsentRequired bool `json:"consentRequired"`
	// Sessions are the remote sessions open now, listed when the local
	// policy shows a session indicator (SEC-24).
	Sessions []Session `json:"sessions"`
}

// Session is one open remote session.
type Session struct {
	ID    string    `json:"id"`
	Kind  string    `json:"kind"`
	User  string    `json:"user"`
	Since time.Time `json:"since"`
}

// Encode turns a message into one line, newline included.
func Encode(message Message) ([]byte, error) {
	line, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("encode a tray message: %w", err)
	}
	if len(line) >= maxMessage {
		return nil, fmt.Errorf("%w: %d bytes", ErrBadMessage, len(line))
	}
	return append(line, '\n'), nil
}

// Decode reads one line, without its newline, and checks it.
func Decode(line []byte) (Message, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var message Message
	if err := decoder.Decode(&message); err != nil {
		return Message{}, fmt.Errorf("%w: %w", ErrBadMessage, err)
	}
	if decoder.More() {
		return Message{}, fmt.Errorf("%w: more than one value", ErrBadMessage)
	}
	if err := message.validate(); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (m Message) validate() error {
	switch m.Type {
	case TypeStatus:
		if m.Status == nil {
			return fmt.Errorf("%w: status without a status", ErrBadMessage)
		}
		return m.Status.validate()
	case TypeAsk:
		if m.ID == "" || !plainText(m.Question) || m.Question == "" {
			return fmt.Errorf("%w: ask needs an ID and a question", ErrBadMessage)
		}
	case TypeAsked, TypeAnswer, TypeEnd:
		if m.ID == "" {
			return fmt.Errorf("%w: %s needs an ID", ErrBadMessage, m.Type)
		}
	case TypePause:
	default:
		return fmt.Errorf("%w: unknown type %q", ErrBadMessage, m.Type)
	}
	if !plainText(m.ID) {
		return fmt.Errorf("%w: the ID isn't plain text", ErrBadMessage)
	}
	return nil
}

func (s Status) validate() error {
	if len(s.Sessions) > maxSessions || !plainText(s.Server) || !plainText(s.Version) {
		return fmt.Errorf("%w: status out of bounds", ErrBadMessage)
	}
	for _, session := range s.Sessions {
		if session.ID == "" || !plainText(session.ID) || !plainText(session.User) ||
			(session.Kind != KindTerminal && session.Kind != KindDesktop) {
			return fmt.Errorf("%w: a session is malformed", ErrBadMessage)
		}
	}
	return nil
}

// plainText is short, valid UTF-8 without control characters: safe to put
// in a menu or a dialog as it is.
func plainText(text string) bool {
	if len(text) > maxText || !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	return true
}

// Reader reads messages from a connection.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader reads from r, refusing lines longer than a message may be.
func NewReader(r io.Reader) *Reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), maxMessage)
	return &Reader{scanner: scanner}
}

// Read returns the next message. It returns io.EOF when the other side
// closed the connection cleanly.
func (r *Reader) Read() (Message, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return Message{}, fmt.Errorf("read a tray message: %w", err)
		}
		return Message{}, io.EOF
	}
	return Decode(r.scanner.Bytes())
}

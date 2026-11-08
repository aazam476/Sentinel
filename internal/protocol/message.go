// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// MessageType says what a message is.
type MessageType string

// The message types. Request, response and event carry actions; the others
// manage the connection and streams. The handshake types are only valid at
// the start of a connection.
const (
	TypeRequest     MessageType = "request"
	TypeResponse    MessageType = "response"
	TypeEvent       MessageType = "event"
	TypeStreamOpen  MessageType = "stream.open"
	TypeStreamClose MessageType = "stream.close"
	TypePing        MessageType = "ping"
	TypePong        MessageType = "pong"

	TypeEnroll    MessageType = "enroll"
	TypeEnrolled  MessageType = "enrolled"
	TypeHello     MessageType = "hello"
	TypeChallenge MessageType = "challenge"
	TypeProve     MessageType = "prove"
	TypeWelcome   MessageType = "welcome"
)

// MaxMessageSize is the largest control message either side accepts. Bulk
// data travels in binary stream frames, so anything bigger is a bug or an
// attack.
const MaxMessageSize = 1 << 20

// Message is the envelope of every control message.
type Message struct {
	Type MessageType `json:"type"`
	// ID identifies a request; its response reuses it. Events and handshake
	// messages carry their own ID too, for logs.
	ID string `json:"id,omitempty"`
	// Action names what a request or event is about, "<area>.<object>.<verb>".
	Action string `json:"action,omitempty"`
	// Payload is the action-specific body, decoded with DecodePayload.
	Payload json.RawMessage `json:"payload,omitempty"`
	// Error is set only on a failed response.
	Error *Error `json:"error,omitempty"`
}

// actionPattern allows lowercase dot-separated names, e.g.
// "docker.container.restart" or "metrics.sample".
var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,4}$`)

// Decode parses and validates one control message.
func Decode(data []byte) (Message, error) {
	if len(data) > MaxMessageSize {
		return Message{}, fmt.Errorf("decode message: %d bytes is over the %d byte limit", len(data), MaxMessageSize)
	}

	var message Message
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return Message{}, fmt.Errorf("decode message: %w", err)
	}
	if decoder.More() {
		return Message{}, errors.New("decode message: unexpected data after the message")
	}
	if err := message.validate(); err != nil {
		return Message{}, fmt.Errorf("decode message: %w", err)
	}
	return message, nil
}

// Encode validates message and returns its JSON.
func Encode(message Message) ([]byte, error) {
	if err := message.validate(); err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	return encoded, nil
}

// validate checks the fields each message type needs.
func (m Message) validate() error {
	switch m.Type {
	case TypeRequest, TypeEvent:
		if !actionPattern.MatchString(m.Action) {
			return fmt.Errorf("%s has an invalid action %q", m.Type, m.Action)
		}
		return m.requireID()
	case TypeResponse:
		return m.requireID()
	case TypeStreamOpen, TypeStreamClose, TypePing, TypePong,
		TypeEnroll, TypeEnrolled, TypeHello, TypeChallenge, TypeProve, TypeWelcome:
		return nil
	default:
		return fmt.Errorf("unknown message type %q", m.Type)
	}
}

func (m Message) requireID() error {
	if !IsID(m.ID) {
		return fmt.Errorf("%s has an invalid ID %q", m.Type, m.ID)
	}
	return nil
}

// DecodePayload decodes message's payload into destination, refusing
// unknown fields so a typo on either side shows up as an error.
func DecodePayload(message Message, destination any) error {
	if len(message.Payload) == 0 {
		return fmt.Errorf("%s %s has no payload", message.Type, message.Action)
	}
	decoder := json.NewDecoder(bytes.NewReader(message.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s %s payload: %w", message.Type, message.Action, err)
	}
	return nil
}

// WithPayload returns message with payload encoded as its JSON payload.
func WithPayload(message Message, payload any) (Message, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Message{}, fmt.Errorf("encode %s payload: %w", message.Type, err)
	}
	message.Payload = encoded
	return message, nil
}

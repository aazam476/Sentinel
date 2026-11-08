// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"context"
	"errors"
	"fmt"

	"github.com/coder/websocket"
)

// Close codes the server uses when it ends an agent's connection, from the
// range WebSocket leaves to applications (4000-4999). The agent reads them
// to tell "try again later" from "this will never work".
const (
	// CloseEnrollmentRefused: the enrollment token was refused.
	CloseEnrollmentRefused websocket.StatusCode = 4001
	// CloseUnknownAgent: the server has no agent with this ID; re-enroll.
	CloseUnknownAgent websocket.StatusCode = 4002
	// CloseRevoked: an admin revoked this agent; re-enroll to come back.
	CloseRevoked websocket.StatusCode = 4003
	// CloseHandshakeFailed: a signature didn't verify, or the handshake
	// broke the protocol.
	CloseHandshakeFailed websocket.StatusCode = 4004
	// CloseUnsupportedVersion: the agent's protocol version is too old or
	// too new for this server.
	CloseUnsupportedVersion websocket.StatusCode = 4005
	// CloseReplaced: the same agent connected again; this older connection
	// is closed.
	CloseReplaced websocket.StatusCode = 4006
	// CloseHeartbeatMissed: nothing arrived for too long.
	CloseHeartbeatMissed websocket.StatusCode = 4007
)

// ErrBinaryFrame means a binary (stream) frame arrived where a control
// message was expected. It is never reassigned.
var ErrBinaryFrame = errors.New("expected a JSON message, got a binary frame")

// ReadMessage reads the next control message from conn.
func ReadMessage(ctx context.Context, conn *websocket.Conn) (Message, error) {
	kind, data, err := conn.Read(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("read message: %w", err)
	}
	if kind != websocket.MessageText {
		return Message{}, ErrBinaryFrame
	}
	return Decode(data)
}

// WriteMessage sends a control message, with payload as its payload unless
// payload is nil.
func WriteMessage(ctx context.Context, conn *websocket.Conn, message Message, payload any) error {
	if payload != nil {
		var err error
		if message, err = WithPayload(message, payload); err != nil {
			return err
		}
	}
	data, err := Encode(message)
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("send %s: %w", message.Type, err)
	}
	return nil
}

// ReadPayload reads the next message, checks it is of type want and decodes
// its payload into destination. The handshake uses it for each step.
func ReadPayload(ctx context.Context, conn *websocket.Conn, want MessageType, destination any) error {
	message, err := ReadMessage(ctx, conn)
	if err != nil {
		return err
	}
	if message.Type != want {
		return fmt.Errorf("expected %s, got %s", want, message.Type)
	}
	return DecodePayload(message, destination)
}

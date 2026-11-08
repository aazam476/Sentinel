// SPDX-License-Identifier: Apache-2.0

// Package protocol defines the messages the server and the agent exchange
// over the agent's WebSocket connection. Both binaries import it, so the
// wire format has exactly one definition.
//
// Every control message is a JSON text frame with the same envelope (see
// Message): a type, an ID, an action name, a payload and, for failed
// responses, an error. High-volume streams (terminal bytes, file chunks) use
// binary frames prefixed with a 4-byte stream ID (see frames.go).
//
// A connection starts with enrollment (first time only) or the handshake
// (every time after), in which both sides prove their identity by signing
// random nonces with their Ed25519 keys (handshake.go). Only then do
// requests, responses and events flow.
//
// The JSON in testdata/protocol is a golden example of every message type.
// The server's and the agent's tests both decode it, so they can't drift
// apart without a test failing.
package protocol

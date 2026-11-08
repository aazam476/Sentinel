// SPDX-License-Identifier: Apache-2.0

// Package hub serves the agents' endpoint, /agent/connect, and keeps track
// of which agents are connected.
//
// Every agent holds one WebSocket open to the server. A connection starts
// with enrollment (first time only) and the handshake of §7.3, in which
// the server and the agent each sign the other's random nonce with their
// Ed25519 key. Only when both signatures check out does the server send
// welcome and treat the agent as online (SEC-21).
//
// After that the agent pings every 15 seconds and sends a metrics sample
// every 10. If nothing arrives for 45 seconds the server closes the
// connection and the agent counts as offline. The server can send the
// agent requests (Request) and gets responses with the same message ID.
//
// Revoking an agent closes its live connection at once (Disconnect), and
// every later handshake is refused.
//
// The endpoint never accepts a browser: a request carrying an Origin header
// is refused before the WebSocket upgrade.
package hub

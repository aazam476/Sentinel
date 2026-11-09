// SPDX-License-Identifier: Apache-2.0

// Package desktop runs remote desktop sessions on the agent (§9.3).
//
// A session shares a Source (a screen, or a test pattern for checking the
// connection) with the browser. The screen is cut into tiles; each frame,
// the tiles that changed are JPEG-encoded and sent as screen updates
// (protocol.EncodeTile). They go over a WebRTC data channel straight to the
// browser when it can reach the agent, and otherwise over the agent's own
// connection to the server, which relays them: slower, but it always
// works. The browser's mouse and keyboard come back the same ways.
//
// Signaling (the WebRTC offer, answer and ICE candidates) travels over the
// server, on the session's stream.
package desktop

// SPDX-License-Identifier: Apache-2.0

// Package relay connects a browser to a stream on an agent: it copies
// terminal input and output between the browser's WebSocket and the
// agent's stream, without looking inside (§7.6).
//
// Between the browser and the server, binary WebSocket messages carry the
// terminal's bytes; text messages carry JSON control messages (see
// Control and Status). When the browser goes away without saying so, the
// stream is detached rather than closed, so the agent keeps the shell for
// 30 seconds and the browser can reattach (Sessions remembers whose
// session it is).
package relay

// SPDX-License-Identifier: Apache-2.0

// Package turn is the server's built-in TURN relay for remote desktop
// (§9.3): when the browser and the agent can't reach each other directly,
// both send their WebRTC traffic through it, over UDP.
//
// Credentials are made per session and expire (the TURN REST scheme): the
// username is the expiry time and a random name, the password an HMAC of
// it with a secret only this process knows. Nothing is stored, and a
// restart makes old credentials useless.
package turn

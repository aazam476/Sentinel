// SPDX-License-Identifier: Apache-2.0

// Package identity keeps what makes an agent itself: its Ed25519 key pair,
// the ID the server gave it, and the server it belongs to, with that
// server's public key.
//
// All of it lives in the agent's state directory, which only root (Linux,
// macOS) or SYSTEM and Administrators (Windows) can read (SEC-20). Anyone
// who could read the private key could impersonate the machine; anyone who
// could change the saved server key could point the agent at an impostor.
//
//   - agent-key: the 32-byte Ed25519 seed, mode 0600
//   - state.json: server URL, agent ID and server public key, mode 0600
//
// The state is written once, at enrollment. Until then Load returns
// ErrNotEnrolled.
package identity

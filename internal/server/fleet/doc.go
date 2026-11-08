// SPDX-License-Identifier: Apache-2.0

// Package fleet keeps the list of managed machines (agents) and the
// enrollment tokens that add new ones.
//
// An admin creates an enrollment token in the UI and pastes the install
// command on a machine. The agent sends the token with its new public key
// (Enroll); if the token is valid, a new agent row is stored and the token
// loses a use. From then on the agent proves who it is with its key in
// every handshake (internal/server/hub), and the server records what the
// agent reports about its machine.
//
// Admins can name machines, tag them and revoke them. A revoked agent stays
// in the database for the audit log, but disappears from the fleet and can
// never connect again.
//
// Everything that changes state is written to the audit log in the same
// transaction as the change.
package fleet

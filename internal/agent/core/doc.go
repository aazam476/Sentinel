// SPDX-License-Identifier: Apache-2.0

// Package core is the agent's connection to its server.
//
// Enroll runs once, at install time: it sends the enrollment token and the
// agent's new public key, and checks that the key the server answers with
// matches the fingerprint in the install command, so a man in the middle is
// caught even when a proxy terminates TLS.
//
// Agent.Run then keeps the agent connected for as long as it runs:
//
//   - it dials the server's /agent/connect endpoint (the agent never
//     listens on a port, SEC-26), checks the server's signature in the
//     handshake and proves its own identity (§7.3);
//   - it pings every 15 seconds, and drops a connection that stays silent
//     for 45, as the server does;
//   - it takes a metrics sample every 10 seconds whether or not it is
//     connected, keeps up to an hour of them, and sends them as soon as it
//     can, oldest first;
//   - it answers the server's requests, refusing anything the local policy
//     file switches off;
//   - when the connection ends, it reconnects after 1 second, doubling the
//     wait each failed attempt up to 60 seconds, give or take 20% so a
//     fleet doesn't reconnect in lockstep after a server restart.
package core

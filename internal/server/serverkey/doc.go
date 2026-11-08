// SPDX-License-Identifier: Apache-2.0

// Package serverkey holds the server's identity: an Ed25519 key pair kept
// in <data dir>/server-key.
//
// Agents save the server's public key when they enroll, and from then on
// check, in every handshake, that the server signs with the matching
// private key. That is how an agent knows it is talking to the real server
// even when a proxy terminates TLS. The key's fingerprint goes into the
// install command, so enrollment itself can be checked too.
package serverkey

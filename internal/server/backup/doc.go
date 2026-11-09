// SPDX-License-Identifier: Apache-2.0

// Package backup writes and reads Sentinel's encrypted backups (§9.10):
// the database, the data directory's keys and certificates, and the
// configuration file, in a tar archive, compressed, and encrypted with a
// key derived from a passphrase.
//
// The file starts with a header naming the format and the key derivation's
// settings, then holds the archive in chunks, each sealed with AES-256-GCM
// (the STREAM construction): every chunk's nonce carries its position and
// whether it is the last, so chunks can't be reordered, dropped or cut off
// without the reader noticing. The header is authenticated with every chunk.
package backup

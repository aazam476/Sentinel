// SPDX-License-Identifier: Apache-2.0

// Package secrets encrypts values the server must store but must not keep
// in plain text, such as TOTP secrets, SMTP passwords and registry
// credentials (SEC-42).
//
// The key is 32 random bytes in <data dir>/secret.key, outside the database,
// so a copy of sentinel.db on its own (a leaked backup, a stolen disk image
// of only the database) reveals nothing. cmd/sentinel-server calls
// LoadOrCreateKey at startup and hands the resulting *Box to the packages
// that store secrets.
//
// Each value is encrypted with AES-256-GCM and a fresh random nonce. Callers
// pass "associated data" naming what the value is and which row it belongs
// to, for example "totp-secret:user:7". Decryption fails if a ciphertext is
// moved to another row or used for another purpose, so someone with write
// access to the database can't swap secrets around.
package secrets

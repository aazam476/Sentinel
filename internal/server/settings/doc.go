// SPDX-License-Identifier: Apache-2.0

// Package settings holds the server settings admins change on the
// Settings page (§9.10): how long metrics history is kept, and which
// release channel agent builds come from. They live in the database's
// settings table, so they survive restarts and go into backups. Settings
// that decide how the server listens (address, TLS, base path, trusted
// proxies) stay in server.toml: the page shows them, read-only, because a
// mistake there could lock everyone out (ADR 0041).
package settings

// SPDX-License-Identifier: Apache-2.0

// Package audit keeps the tamper-evident record of everything that happens
// on the server: logins, sessions, state-changing actions, user and role
// changes, and policy denials (SEC-40, SEC-41).
//
// Other packages call Log.Record (or Log.RecordInTx, to record inside the
// same transaction as the change itself, so the change and its audit entry
// are saved together or not at all). The API lists entries for admins and
// exports them as CSV or JSON; `sentinel-server audit verify` checks the
// chain from the command line.
//
// How tampering is detected: every row stores the hash of the previous row
// and its own hash, which is SHA-256 over its content plus that previous
// hash. Changing, removing or reordering any row changes its hash, which no
// longer matches what the next row says, so Verify finds the first broken
// link. SQLite triggers also refuse UPDATE and DELETE on the table.
//
// The log is append-only, so an action is never "updated" with its result.
// A long-running action records one entry when it starts and another with
// its outcome. See docs/decisions/0009-audit-log.md.
package audit

# 0009 — Audit log design

**Date:** 2026-11-07 · **Status:** Accepted

## Context

The audit log must be append-only and tamper-evident: each row stores `previous_hash` and
`hash`, the SHA-256 over the row's content and the previous hash (SEC-40). Admins must be
able to view and export it, and a `verify` command must check the chain (SEC-41). The
request flow for agent actions says the server "updates the audit entry with the outcome",
which an append-only log can't do.

## Decision

- **Append-only, enforced twice.** The code never updates or deletes rows, and SQLite
  triggers make `UPDATE` and `DELETE` on `audit_log` fail.
- **Outcomes are new rows, not updates.** An action that finishes later (an agent request,
  a terminal session) records an entry with outcome `started`, then another with `success`,
  `failure` or `denied`. Instant actions record one entry with their outcome.
- **Hash input.** A JSON object with fixed keys in a fixed order: `id`, `ts`, `user_id`,
  `username`, `agent_id`, `action`, `details` (the exact stored text), `outcome`, `ip`,
  `previous_hash`. JSON makes the field boundaries unambiguous. The first row's previous hash
  is 64 zeros. This encoding can never change without breaking verification of old rows.
- **Explicit IDs.** Each row's ID is the previous row's ID plus one, set by the code and
  covered by the hash, so a deleted row shows up as a gap.
- **Atomic with the change.** `RecordInTx` writes the entry in the same transaction as the
  change it describes. Writes are serialized by the store's single writer, so the chain
  never forks.
- **No foreign keys** to users or agents, so history survives their deletion; `username`
  keeps the name used at the time.
- **Verify and export** are available from the command line (`sentinel-server audit
  verify`, `sentinel-server audit export --format csv|json`), and through the API for admins.
- **CSV export** prefixes values that start with `=`, `+`, `-`, `@`, tab or carriage return
  with an apostrophe, so a malicious username can't run as a spreadsheet formula.

## Consequences

- Any change to a stored row, a removed row, or a reordering is detected and the first bad
  entry is named.
- The chain can't stop someone who can write to the database *and* recompute every later
  hash. Comparing the latest hash against a copy kept elsewhere (an export, a previous
  `verify` output) detects that. An HMAC keyed with `secret.key` would close the gap; it is
  left out because the requirement asks for plain SHA-256 and that keeps the chain checkable
  without the key.
- Audit entries never contain passwords, tokens or secrets; callers are responsible for
  keeping them out of `details`.

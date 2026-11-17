# 0049 — Machines can't grow the alerts table without bound

**Date:** 2026-11-17 · **Status:** Accepted

## Context

Storage, container and failed-script alerts carry text from the machine: names of pools,
disks and containers, and error output. Nothing limited how long that text was, how many
alerts one report raised, or how many resolved alerts were kept. An enrolled machine could
report new names every minute and add rows forever.

## Decision

- One report raises at most 100 alerts. Subjects are trimmed to 200 bytes and messages to
  4,000, on a character boundary, ending in "…".
- Every hour the server deletes resolved alerts older than 90 days, and keeps at most the
  newest 500 resolved alerts per machine. Firing alerts are never deleted.

## Consequences

- A machine with more than 100 simultaneous problems shows the first 100.
- Alert history older than 90 days is gone; the audit log keeps who acknowledged what.

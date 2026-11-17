# 0048 — Refused attempts are audited at most once a minute per network

**Date:** 2026-11-17 · **Status:** Accepted

## Context

SEC-05 and SEC-40 audit every failed and refused sign-in, and every refused enrollment. Each
one wrote its own row, so a client that never signed in could write rows as fast as it could
send requests, from as many addresses as it has (an IPv6 client usually has a whole /64), until
the disk was full.

## Decision

- `audit.Throttle` records a network's first refused attempt of a kind, then at most one a
  minute. The next row it records says how many it stood in for (`heldBack`), so every refusal
  is still accounted for. IPv6 addresses are grouped by /64.
- Each kind is also capped at 60 rows a minute across every network, and at most 10,000
  networks are remembered.
- It applies to rate-limited and locked-account sign-in refusals, failed sign-ins for usernames
  that don't exist (all one kind), and refused enrollments.
- A failed sign-in against a real account still gets its own row: the account locks after a
  few, so those are bounded, and they are what an admin investigates.

## Consequences

- The audit log grows by at most about 120 rows a minute from clients that aren't signed in.
- Under a flood, the rows show the first attempts and counts, not every address.

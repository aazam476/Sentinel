# 0052 — The TURN relay won't reach the server itself, and has quotas

**Date:** 2026-11-17 · **Status:** Accepted

## Context

The remote desktop relay (ADR 0035) gives every signed-in user with a desktop session
credentials that relay UDP to any address the relay permits. It refused loopback, link-local,
multicast and unspecified addresses, but relayed to the server's own other addresses, which
reach whatever the server listens on from a source the server trusts. Nothing limited how many
allocations one set of credentials could hold.

## Decision

- The relay refuses peers at any of the server's own interface addresses and at the address it
  relays from. Private addresses stay allowed: agents usually live on them.
- One set of credentials (one desktop session) may hold at most 4 allocations, and the relay
  at most 200. Credentials still expire as before; bandwidth is bounded by the relay's port
  range.

## Consequences

- An agent running on the server's own machine can't use the relay; it connects directly.

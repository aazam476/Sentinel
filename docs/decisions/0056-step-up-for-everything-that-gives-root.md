# 0056 — A recent second factor for everything that gives root on a machine

**Date:** 2026-11-17 · **Status:** Accepted

## Context

SEC-07 asks for a recent second-factor check (within 15 minutes) before the terminal, remote
desktop, user changes and, since ADR 0043, scripts on several machines. Other actions give the
same power with no check: a script on one machine, saving or deploying a compose stack (which
can mount the host's disk), changing files as root, hosting an agent build that machines then
update to, and creating an enrollment token that adds a machine the attacker controls.

## Decision

- These need a recent second factor, or the password for a user without one: running any
  script, switching any schedule on, saving a script, writing, renaming, deleting, creating and
  uploading files, saving and deploying stacks, uploading and fetching agent builds, and
  creating enrollment tokens. Installing package updates already did (ADR 0042).
- On the server, `withStepUp` wraps those routes, so the check happens before anything else.
- In the browser, the pages that already asked keep their own dialog and retry. The rest are
  marked `stepUp` and share one dialog in the app shell, which opens when the server asks and
  then says to do it again.

## Consequences

- Someone who leaves a signed-in browser for 15 minutes can't be impersonated into root on a
  machine without the second factor.
- Reading stays unprompted: listing files, viewing stacks and logs.

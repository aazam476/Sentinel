# 0037 — First-time setup without a setup token

**Date:** 2026-11-11 · **Status:** Accepted · **Supersedes:** the setup token in ADR 0011

## Context

SEC-01 asked for a one-time setup token: printed to the log and written to
`<data dir>/setup-token` on first start, required by the setup page, deleted afterwards. It
stops someone who reaches a fresh server before its owner from creating the first admin.

In practice it was the step people tripped over. With Docker it meant reading container logs
before the page would do anything; with the install script it meant waiting for a file to
appear; with a reverse proxy in front it meant a shell on the host. The maintainer asked for
it to go.

## Decision

Setup asks only for the first admin's username and password, then TOTP enrollment and recovery
codes, as before (SEC-02). It is open exactly while no user exists: once the admin is created
in the same transaction as their confirmed TOTP secret, `BeginSetup` and `CompleteSetup`
return `setup_not_available`. A half-finished setup still writes nothing, so it can't leave an
admin without 2FA, and two people racing through setup can't both win: the transaction that
creates the admin checks again that no user exists.

The server logs "no admin exists yet; open the web UI to create one" at each start until
setup is done. A `setup-token` file left by an older server is deleted at start. SEC-01 is
removed from the security checklist.

## Consequences

- Whoever opens the page first owns the server. INSTALL.md, the install script and
  `compose.yaml` now say to create the admin straight away and to keep the port off the
  internet until then. If someone else got there first, the fix is to delete the data
  directory and start again: nothing on the server is theirs to keep yet.
- A server exposed to the internet before setup is the one case the token covered that this
  doesn't. That's a spec change to SEC-01, listed in the final report.
- `reset-admin` (SEC-08) is unchanged: it still needs root or the data directory's owner.

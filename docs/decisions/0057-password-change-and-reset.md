# 0057 — Users change their own password; admins reset others'

**Date:** 2026-11-17 · **Status:** Accepted

## Context

There was no way to change a password after it was set, short of `sentinel-server reset-admin`,
which only resets an admin and also removes their second factors.

## Decision

- **Account → Password** changes the user's own, given the current one. Wrong guesses count
  against the sign-in rate limit and lockout (SEC-05). Their other sessions end; this one
  stays.
- **Users → Reset password** sets another user's, for admins with a recent second factor
  (SEC-07). That user's sessions end and any lockout is lifted; their second factors stay.
- Both check the password policy (SEC-02) and are audited (`auth.password.change`,
  `user.password.reset`). An admin changes their own on the Account page, not by reset.

## Consequences

- A forgotten password no longer needs the command line, unless it is the only admin's.

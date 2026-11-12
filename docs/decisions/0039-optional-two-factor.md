# 0039 — Two-factor authentication: optional by default, required by an admin

**Date:** 2026-11-12 · **Status:** Accepted · **Amends:** ADR 0011, ADR 0012 (SEC-04)

## Context

SEC-04 made 2FA mandatory: setup enrolled TOTP, and every user without a second factor was
sent to enroll before anything else. The maintainer asked for 2FA to be optional, as a server
setting that is off for new installs.

## Decision

- **One server-wide setting, `auth.require_two_factor`, off by default.** It lives in the
  `settings` table, is changed on the **Users** page by admins (`PUT /api/settings/security`,
  which needs a recent check, SEC-07) and is audited as `settings.require_two_factor`.
- **Off:** setup shows the authenticator app step with **Skip for now**; a skipped setup
  creates the admin with no TOTP and no recovery codes. A user without a second factor is
  signed in by their password (`nextStep: "signed_in"`, audited as `auth.login` with method
  `password`). Anyone can add an authenticator app or passkeys from **Account**, and remove
  them; removing the last one also deletes the recovery codes, which only work with 2FA. A user
  who has a second factor is always asked for it: the setting never weakens an account that
  set one up.
- **On:** today's SEC-04 behaviour. Turning it on needs the admin to use 2FA themselves (else
  they'd be sent to enroll at once); it signs out every user without a second factor, who
  enrolls at their next sign-in, and nobody can remove their last second factor. Setup can't
  skip 2FA while it is on, as on a server restored from a backup of one where it was.
- **Step-up without a second factor re-asks the password** (`POST /api/auth/step-up` with
  `password`), rate-limited and audited like a login (SEC-05). A user with a second factor
  can't step up with the password. The session's `last_2fa_at` is the time of the last check
  of either kind.
- **The Users page shows who has no 2FA**, and says how many will be signed out before the
  requirement is turned on.

## Consequences

- A spec change to SEC-04: 2FA is no longer mandatory unless an admin requires it.
- Until then, an account without 2FA is protected by its password alone: Argon2id hashing,
  the password rules (SEC-02), rate limiting and step-up still apply. INSTALL.md recommends
  setting up an authenticator app and points to the switch.
- Existing tests of the mandatory behaviour now run with the requirement on; new tests cover
  the optional mode, the switch and the password step-up.

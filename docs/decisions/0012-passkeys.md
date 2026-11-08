# 0012 — Passkeys as a second factor

**Date:** 2026-11-08 · **Status:** Accepted

## Context

2FA is mandatory, and "passkeys can replace TOTP" (SEC-04). How passkeys fit the sign-in
flow, step-up checks and account recovery wasn't specified.

## Decision

- **A second factor, not a password replacement.** Sign-in is still username and password
  first. A passkey is then one of the ways to finish, next to a TOTP code and a recovery code.
  This keeps one sign-in flow for everyone and leaves the password-plus-second-factor rules,
  rate limiting and audit trail unchanged.
- **Enrollment.** A user without any second factor (a new user, or an admin after
  `reset-admin`) can choose a passkey instead of TOTP. Like TOTP enrollment, that signs them in
  and issues ten recovery codes.
- **Adding more passkeys** needs a second-factor check within the last 15 minutes (SEC-07),
  because a new passkey is a new way into the account. **Removing** one needs the same, and
  the last remaining second factor can't be removed.
- **Step-up** accepts a passkey as well as a TOTP code.
- **Relying party.** Passkeys belong to the public URL's host name, or, without one, to the
  host the browser used. Browsers don't allow passkeys on IP addresses, so a server reached
  only by IP can use TOTP but not passkeys; development uses `localhost` for this reason.
- **Checks.** The `go-webauthn` library verifies the challenge, origin, relying party and
  signature. Each challenge is held in memory for five minutes and can be answered once. A
  signature counter that goes backwards (a sign of a cloned key) is refused. User verification
  is preferred, not required, so security keys without a PIN still work. Attestation is not
  checked: any authenticator the user owns is acceptable.
- **Storage.** The `passkeys` table keeps the credential ID, public key, counter and name, plus
  the library's full credential record as JSON. Each user gets a random 64-byte WebAuthn user
  handle the first time they add a passkey.
- **Failures** count towards login rate limiting and are audited, like wrong codes.

## Consequences

- Users can sign in without typing codes, and phishing sites can't reuse a passkey answer.
- A server reached only by IP address can't offer passkeys.
- Pending passkey challenges are lost on restart; the user simply tries again.

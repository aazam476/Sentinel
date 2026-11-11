# 0011 — Sign-in flow, sessions and login rate limiting

**Date:** 2026-11-07 · **Status:** Accepted; the setup token was dropped by ADR 0037

## Context

2FA is mandatory for every user (SEC-04), the first admin must be created with a setup token
and TOTP enrollment (SEC-01, SEC-02), failed logins must back off exponentially and lock an
account after 10 failures (SEC-05), sessions have idle and absolute timeouts (SEC-06), and
sensitive actions need a fresh 2FA check (SEC-07). Several details weren't specified.

## Decision

**First-boot setup.** The token is 32 random bytes in base32 (52 characters), written to
`<data dir>/setup-token` (mode 0600) and printed at every start until setup is done. Setup has
two steps: the token, username and password give a TOTP secret to scan; the token, a setup ID
and a correct TOTP code then create the admin, the confirmed TOTP secret and ten recovery codes
in one transaction. Until the code is confirmed nothing is written, so an abandoned setup
leaves no admin without 2FA. Unfinished setups expire after 15 minutes. The token file is
deleted once setup completes, and any leftover file is deleted at the next start.

**Two-step sign-in.** A correct password creates a session that can only finish signing in. It
expires after 10 minutes. The next step is either a TOTP or recovery code, or, for a user
without 2FA (created by an admin), TOTP enrollment. Finishing either replaces the session with
a new one and a new token, so a token seen before sign-in is useless afterwards.

**TOTP.** 6 digits, 30-second steps, SHA-1 (what every authenticator app supports), accepting
one step either side for clock drift. The last accepted step is stored, so each code works
once. Secrets are encrypted with the key in `secret.key`, bound to their user.

**Recovery codes.** Ten codes of 80 random bits, shown as `ABCD-EFGH-IJKL-MNOP`, stored as
SHA-256 hashes, each usable once. Case, dashes and spaces are ignored when entered. They can
finish a sign-in but not a step-up check.

**Rate limiting.** Every failed password, TOTP code, recovery code, enrollment code or step-up
code counts against both the client's IP address and the username (case-insensitive, whether
or not it exists). After three failures in a row, each further attempt must wait 1 s, then
2 s, 4 s and so on, up to 15 minutes. Ten failures in a row lock the username for 15 minutes,
even with the right password. The lock is stored in `users.locked_until`, so it survives a
restart; the backoff counters are in memory and are forgotten after an hour without failures.
A successful sign-in resets the username's counter but not the IP address's, so an attacker
can't clear their own counter by signing in to an account of their own between guesses.
Every failure and refusal is audited.

**Sessions.** 256-bit random tokens, stored as SHA-256 hashes. Idle timeout 12 hours, absolute
timeout 7 days. The last-seen time is written at most once a minute per session. Expired
sessions are deleted when they are next presented. Users can list their signed-in sessions and
revoke any of them. Disabling a user deletes their sessions.

**Step-up.** Changing users or roles (and later, terminals, desktop sessions and scripts on
more than one machine) needs a TOTP check within the last 15 minutes. A step-up check restarts
that window for the session.

**Users.** Usernames are 1 to 64 letters, digits, dots, dashes or underscores, starting with a
letter or digit, unique regardless of letter case. A change that would leave no enabled admin
is refused.

## Consequences

- Nothing a user can do bypasses 2FA, including users created by an admin.
- A locked-out legitimate user waits 15 minutes; an admin can't unlock them early yet.
- Usernames that don't exist get the same answers and delays as real ones.

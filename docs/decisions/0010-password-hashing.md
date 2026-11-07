# 0010 — Password hashing with Argon2id

**Date:** 2026-11-07 · **Status:** Accepted

## Context

Passwords must be hashed with Argon2id, starting from memory 64 MiB, 3 iterations and
parallelism 2, tuned and documented here (SEC-03).

## Decision

- **Settings:** memory 64 MiB, 3 iterations, parallelism 2, 16-byte random salt, 32-byte key.
  On a 4-core cloud VM one hash takes about 50 to 150 ms, which is in the range OWASP
  recommends for interactive logins and well within what a login page can absorb. Raising
  memory to 128 MiB added roughly 25% time for every login while doubling the memory a burst
  of logins needs, so we kept 64 MiB.
- **Format:** the standard encoded string
  `$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>`. Each stored hash carries its own settings,
  so future tuning applies to newly set passwords without breaking old ones.
- **At most two hashes at a time.** Each hash holds 64 MiB, so an unbounded burst of login
  attempts could exhaust the server's memory. Extra requests wait their turn (or give up when
  the client disconnects).
- **Unknown usernames cost the same.** A login for a username that doesn't exist is checked
  against a dummy hash with the same settings, so response time doesn't reveal which
  usernames exist.
- **Password rules (SEC-02):** at least 12 characters (counted as characters, not bytes), at
  most 256, not the username, and not on the bundled list of 46,886 common passwords of 12+
  characters (compared case-insensitively). See
  `internal/server/auth/commonpasswords/README.md` for its source.
- **Other secrets use SHA-256.** Session tokens (256 bits) and recovery codes (80 bits) are
  random enough that a fast hash is safe: nobody can guess an input that matches.

## Consequences

- A stolen database doesn't reveal passwords without a costly per-password attack.
- Logins take a noticeable fraction of a second by design, and the tests that sign in many
  times are slower than most.

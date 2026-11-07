-- SPDX-License-Identifier: Apache-2.0
--
-- 0003: user accounts, two-factor authentication and sessions.
--
-- Times are UTC Unix milliseconds. Secrets are never stored in plain text:
-- passwords as Argon2id hashes, session tokens and recovery codes as SHA-256
-- hashes, TOTP secrets encrypted with the key in secret.key.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    -- NOCASE: "Alice" and "alice" are the same user.
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL CHECK (role IN ('viewer', 'operator', 'admin')),
    created_at    INTEGER NOT NULL,
    disabled_at   INTEGER,
    -- Set when repeated failed logins lock the account; survives restarts.
    locked_until  INTEGER
) STRICT;

CREATE TABLE totp_secrets (
    user_id          INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret_encrypted BLOB    NOT NULL,
    -- NULL until the user proves their app works by entering a code.
    confirmed_at     INTEGER,
    -- The last 30-second step a code was accepted for, so no code works twice.
    last_used_step   INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE TABLE recovery_codes (
    id        INTEGER PRIMARY KEY,
    user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash TEXT    NOT NULL,
    used_at   INTEGER
) STRICT;

CREATE INDEX recovery_codes_by_user ON recovery_codes (user_id);

CREATE TABLE sessions (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    -- NULL while the session has only passed the password step.
    last_2fa_at  INTEGER,
    ip           TEXT    NOT NULL,
    user_agent   TEXT    NOT NULL
) STRICT;

CREATE INDEX sessions_by_user ON sessions (user_id);

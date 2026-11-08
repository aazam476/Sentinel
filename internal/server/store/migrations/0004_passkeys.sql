-- SPDX-License-Identifier: Apache-2.0
--
-- 0004: passkeys (WebAuthn credentials), which can replace TOTP as a
-- user's second factor.
--
-- webauthn_handle is the random "user handle" that authenticators store
-- with each passkey. It is created when a user adds their first passkey and
-- never changes, so all of a user's passkeys share it.

ALTER TABLE users ADD COLUMN webauthn_handle BLOB;

CREATE TABLE passkeys (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id   BLOB    NOT NULL UNIQUE,
    public_key      BLOB    NOT NULL,
    sign_count      INTEGER NOT NULL,
    name            TEXT    NOT NULL,
    -- Everything else the WebAuthn library needs to check later sign-ins
    -- (flags, transports, attestation), as it serializes it.
    credential_json TEXT    NOT NULL,
    created_at      INTEGER NOT NULL,
    last_used_at    INTEGER
) STRICT;

CREATE INDEX passkeys_by_user ON passkeys (user_id);

-- SPDX-License-Identifier: Apache-2.0
--
-- 0006: Docker registry logins and image update checks (M4).
--
-- A registry login's password is encrypted with the server's secret key
-- (SEC-42) and sent to an agent only for the one pull or check that needs
-- it. image_updates keeps the last check of each image tag on each
-- machine, so the UI can show "update available" between checks.

CREATE TABLE registry_credentials (
    id             INTEGER PRIMARY KEY,
    -- The registry's host, e.g. ghcr.io; docker.io for Docker Hub.
    server_address TEXT    NOT NULL UNIQUE,
    username       TEXT    NOT NULL,
    -- AES-256-GCM sealed password, bound to server_address.
    password       BLOB    NOT NULL,
    created_by     INTEGER NOT NULL REFERENCES users (id),
    created_at     INTEGER NOT NULL
) STRICT;

CREATE TABLE image_updates (
    agent_id         INTEGER NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    -- The image's tag as the machine names it, e.g. nginx:1.27.
    reference        TEXT    NOT NULL,
    registry_digest  TEXT    NOT NULL,
    update_available INTEGER NOT NULL,
    -- An error from the last check (rate limited, unknown registry), or ''.
    error            TEXT    NOT NULL,
    checked_at       INTEGER NOT NULL,
    PRIMARY KEY (agent_id, reference)
) STRICT;

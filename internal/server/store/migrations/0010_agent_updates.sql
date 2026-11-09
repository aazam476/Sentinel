-- SPDX-License-Identifier: Apache-2.0
--
-- 0010: agent self-updates (M7, §11.3).
--
-- The last update each machine was asked to make, and how it went. The
-- builds themselves are files in <data dir>/updates/, each with its
-- release signature; whether machines update by themselves is the setting
-- "updates.auto".

CREATE TABLE agent_updates (
    agent_id    INTEGER PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    version     TEXT    NOT NULL,
    -- updating, updated, failed or rolled_back.
    state       TEXT    NOT NULL,
    error       TEXT    NOT NULL DEFAULT '',
    started_at  INTEGER NOT NULL,
    finished_at INTEGER
) STRICT;

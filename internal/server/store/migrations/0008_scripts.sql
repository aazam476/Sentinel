-- SPDX-License-Identifier: Apache-2.0
--
-- 0008: scripts, their runs and their schedules (M6, §9.8).
--
-- A script is in the library. Running it on some machines makes a batch,
-- which keeps a copy of the script as it was run (so editing or deleting
-- the script later doesn't change history) and one run per machine with
-- its output (up to 1 MB), exit code and times. A schedule runs a script
-- on a target at the times a cron expression gives, in its time zone.

CREATE TABLE scripts (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL UNIQUE,
    description     TEXT    NOT NULL,
    -- bash, sh, powershell or python.
    language        TEXT    NOT NULL,
    body            TEXT    NOT NULL,
    -- any, linux, darwin or windows: machines of another system are skipped.
    target_os       TEXT    NOT NULL,
    timeout_seconds INTEGER NOT NULL,
    created_by      INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
) STRICT;

CREATE TABLE schedules (
    id           INTEGER PRIMARY KEY,
    script_id    INTEGER NOT NULL REFERENCES scripts (id) ON DELETE CASCADE,
    cron         TEXT    NOT NULL,
    -- An IANA time zone, e.g. Europe/Berlin, that the cron times are in.
    timezone     TEXT    NOT NULL,
    -- agent (one machine's ID), tag (a tag) or agents (IDs, comma-separated).
    target_kind  TEXT    NOT NULL,
    target_value TEXT    NOT NULL,
    enabled      INTEGER NOT NULL,
    created_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at   INTEGER NOT NULL,
    -- When it runs next; NULL while disabled or if it never matches.
    next_run_at  INTEGER,
    last_run_at  INTEGER
) STRICT;

CREATE TABLE script_batches (
    id          INTEGER PRIMARY KEY,
    script_id   INTEGER REFERENCES scripts (id) ON DELETE SET NULL,
    script_name TEXT    NOT NULL,
    language    TEXT    NOT NULL,
    body        TEXT    NOT NULL,
    -- Who started it, or the schedule that did.
    started_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
    schedule_id INTEGER REFERENCES schedules (id) ON DELETE SET NULL,
    -- The target as people read it, e.g. "tag homelab".
    target      TEXT    NOT NULL,
    started_at  INTEGER NOT NULL
) STRICT;

CREATE TABLE script_runs (
    id               INTEGER PRIMARY KEY,
    batch_id         INTEGER NOT NULL REFERENCES script_batches (id) ON DELETE CASCADE,
    agent_id         INTEGER NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    -- running, succeeded, failed, timed_out, offline, skipped or canceled.
    state            TEXT    NOT NULL,
    exit_code        INTEGER,
    error            TEXT    NOT NULL DEFAULT '',
    output           TEXT    NOT NULL DEFAULT '',
    output_truncated INTEGER NOT NULL DEFAULT 0,
    started_at       INTEGER NOT NULL,
    finished_at      INTEGER
) STRICT;

CREATE INDEX script_runs_by_batch ON script_runs (batch_id);
CREATE INDEX script_batches_by_time ON script_batches (started_at);
CREATE INDEX schedules_due ON schedules (next_run_at) WHERE enabled = 1;

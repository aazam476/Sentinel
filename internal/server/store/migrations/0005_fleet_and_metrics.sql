-- SPDX-License-Identifier: Apache-2.0
--
-- 0005: managed machines (agents), the tokens that enroll them, and their
-- metrics.
--
-- An agent has two IDs. public_id is a ULID: the agent stores it, the
-- protocol and the API use it, and it stays unique even if the database is
-- restored from an old backup. id is the small internal number that other
-- tables (and the audit log) refer to.
--
-- Revoked agents keep their row, so the audit log and old metrics still
-- name them; they disappear from the fleet and can never connect again.

CREATE TABLE enrollment_tokens (
    id         INTEGER PRIMARY KEY,
    -- SHA-256 of the token; the token itself is shown once and never stored.
    token_hash TEXT    NOT NULL UNIQUE,
    -- A note for the admin, e.g. "office NAS".
    note       TEXT    NOT NULL,
    created_by INTEGER NOT NULL REFERENCES users (id),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    max_uses   INTEGER NOT NULL,
    use_count  INTEGER NOT NULL DEFAULT 0,
    revoked_at INTEGER
) STRICT;

CREATE TABLE agents (
    id                  INTEGER PRIMARY KEY,
    public_id           TEXT    NOT NULL UNIQUE,
    -- Empty until an admin names the machine; the UI shows the hostname.
    display_name        TEXT    NOT NULL DEFAULT '',
    hostname            TEXT    NOT NULL,
    os                  TEXT    NOT NULL,
    arch                TEXT    NOT NULL,
    agent_version       TEXT    NOT NULL,
    protocol_version    INTEGER NOT NULL,
    public_key          BLOB    NOT NULL,
    enrollment_token_id INTEGER REFERENCES enrollment_tokens (id),
    enrolled_at         INTEGER NOT NULL,
    last_seen_at        INTEGER,
    revoked_at          INTEGER,
    maintenance_until   INTEGER,
    -- What the agent last reported about its machine (protocol.SystemInfo),
    -- as JSON; '{}' until the first report.
    system_info_json    TEXT    NOT NULL DEFAULT '{}'
) STRICT;

CREATE TABLE agent_tags (
    agent_id INTEGER NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    tag      TEXT    NOT NULL,
    PRIMARY KEY (agent_id, tag)
) STRICT, WITHOUT ROWID;

CREATE INDEX agent_tags_by_tag ON agent_tags (tag);

-- Which features the machine's local policy file allows. The agent
-- reports this; the server can only display it.
CREATE TABLE agent_capabilities (
    agent_id   INTEGER NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    capability TEXT    NOT NULL,
    enabled    INTEGER NOT NULL,
    reason     TEXT    NOT NULL,
    PRIMARY KEY (agent_id, capability)
) STRICT, WITHOUT ROWID;

-- Metrics are stored "long": one row per agent, time and metric name, so a
-- new metric needs no migration. Raw samples arrive every 10 seconds; the
-- scheduler averages them into 1-minute and 1-hour rows and deletes old
-- rows (24 hours raw, 14 days per minute, 1 year per hour).
--
-- The primary key leads with agent and metric because every chart reads
-- one metric of one agent over a time range. ts is UTC Unix milliseconds,
-- the start of the bucket in the rollup tables.

CREATE TABLE metrics_raw (
    agent_id INTEGER NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    metric   TEXT    NOT NULL,
    ts       INTEGER NOT NULL,
    value    REAL    NOT NULL,
    PRIMARY KEY (agent_id, metric, ts)
) STRICT, WITHOUT ROWID;

CREATE TABLE metrics_1m (
    agent_id INTEGER NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    metric   TEXT    NOT NULL,
    ts       INTEGER NOT NULL,
    value    REAL    NOT NULL,
    min      REAL    NOT NULL,
    max      REAL    NOT NULL,
    PRIMARY KEY (agent_id, metric, ts)
) STRICT, WITHOUT ROWID;

CREATE TABLE metrics_1h (
    agent_id INTEGER NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    metric   TEXT    NOT NULL,
    ts       INTEGER NOT NULL,
    value    REAL    NOT NULL,
    min      REAL    NOT NULL,
    max      REAL    NOT NULL,
    PRIMARY KEY (agent_id, metric, ts)
) STRICT, WITHOUT ROWID;

-- Retention deletes by time across all agents.
CREATE INDEX metrics_raw_by_time ON metrics_raw (ts);
CREATE INDEX metrics_1m_by_time ON metrics_1m (ts);
CREATE INDEX metrics_1h_by_time ON metrics_1h (ts);

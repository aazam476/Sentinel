-- SPDX-License-Identifier: Apache-2.0
--
-- 0007: alerts (M5 raises storage alerts; M6 adds rules and notifications).
--
-- An alert is one condition on one machine, such as "pool tank is
-- DEGRADED". While the condition holds the alert is firing; when it clears
-- the alert is resolved and kept as history. A condition firing again
-- later is a new alert. The partial unique index keeps one firing alert
-- per condition, which is what deduplicates them.

CREATE TABLE alerts (
    id          INTEGER PRIMARY KEY,
    agent_id    INTEGER NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    -- What kind of condition, e.g. zfs.pool.health or smart.failing.
    kind        TEXT    NOT NULL,
    -- What it is about on the machine: a pool, dataset or disk name.
    subject     TEXT    NOT NULL,
    -- warning or critical.
    severity    TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    -- firing or resolved.
    state       TEXT    NOT NULL,
    started_at  INTEGER NOT NULL,
    resolved_at INTEGER
) STRICT;

CREATE UNIQUE INDEX alerts_one_firing ON alerts (agent_id, kind, subject) WHERE state = 'firing';
CREATE INDEX alerts_by_agent ON alerts (agent_id, started_at);

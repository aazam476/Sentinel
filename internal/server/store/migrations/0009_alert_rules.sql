-- SPDX-License-Identifier: Apache-2.0
--
-- 0009: alert rules, notification channels, acknowledgement and
-- maintenance mode (M6, §9.9).
--
-- A rule says what to watch (a metric over a threshold for some minutes,
-- a machine offline, storage health, failed scheduled scripts), on which
-- machines, and which channels hear about it. Each alert now belongs to
-- the rule that raised it. A channel's settings (SMTP password, webhook
-- URL with its token) are encrypted with the secret key (SEC-42). A
-- machine in maintenance (agents.maintenance_until, from 0005) raises
-- alerts as usual, but nobody is notified.

CREATE TABLE alert_rules (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    -- metric, offline, storage or script_failed.
    type         TEXT    NOT NULL,
    config_json  TEXT    NOT NULL,
    -- all, tag (target_value is a tag) or agent (a machine's ID).
    target_kind  TEXT    NOT NULL,
    target_value TEXT    NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL,
    created_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE notification_channels (
    id               INTEGER PRIMARY KEY,
    -- email, webhook, ntfy or discord.
    type             TEXT    NOT NULL,
    name             TEXT    NOT NULL UNIQUE,
    -- AES-256-GCM sealed JSON settings, bound to the channel's type.
    config_encrypted BLOB    NOT NULL,
    enabled          INTEGER NOT NULL,
    created_at       INTEGER NOT NULL,
    last_sent_at     INTEGER,
    -- Why the last notification failed, or ''.
    last_error       TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE alert_rule_channels (
    rule_id    INTEGER NOT NULL REFERENCES alert_rules (id) ON DELETE CASCADE,
    channel_id INTEGER NOT NULL REFERENCES notification_channels (id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, channel_id)
) STRICT;

ALTER TABLE alerts ADD COLUMN rule_id INTEGER REFERENCES alert_rules (id) ON DELETE SET NULL;
ALTER TABLE alerts ADD COLUMN acknowledged_by INTEGER REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE alerts ADD COLUMN acknowledged_at INTEGER;

-- Storage alerts were always on in M5; this rule keeps them on, for every
-- machine, and makes their threshold and channels editable.
INSERT INTO alert_rules (name, type, config_json, target_kind, target_value, enabled, created_at)
VALUES ('Storage health', 'storage', '{"capacityPercent":80}', 'all', '', 1, 0);

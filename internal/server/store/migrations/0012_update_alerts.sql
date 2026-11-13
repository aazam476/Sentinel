-- SPDX-License-Identifier: Apache-2.0
--
-- 0012: updates-available alerts (§9.9).
--
-- Machines with operating system updates waiting raise an alert by an
-- "updates" rule, checked once a day; the rule's channels get one digest a
-- day naming every such machine. Like the storage and container rules, it
-- watches every machine and can be edited or switched off.
INSERT INTO alert_rules (name, type, config_json, target_kind, target_value, enabled, created_at)
VALUES ('Updates available', 'updates', '{}', 'all', '', 1, 0);

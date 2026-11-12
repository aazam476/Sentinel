-- SPDX-License-Identifier: Apache-2.0
--
-- 0011: container alerts (§9.9).
--
-- Containers that stop with a non-zero exit code or keep restarting raise
-- alerts by a "container" rule, as storage problems do by a "storage" one.
-- This rule turns them on for every machine, as the storage rule did; its
-- target and channels can be edited, or it can be switched off.
INSERT INTO alert_rules (name, type, config_json, target_kind, target_value, enabled, created_at)
VALUES ('Containers', 'container', '{}', 'all', '', 1, 0);

-- SPDX-License-Identifier: Apache-2.0
--
-- 0001: the first schema. Server-wide settings that can be changed at run
-- time are stored here as one JSON value per key. Every feature adds its own
-- tables in later migrations.

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL
) STRICT;

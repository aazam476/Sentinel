-- SPDX-License-Identifier: Apache-2.0
--
-- 0002: the audit log. Rows are only ever added: the triggers below make
-- UPDATE and DELETE fail, and each row's hash covers its content plus the
-- previous row's hash, so editing the file outside SQLite breaks the chain
-- and `sentinel-server audit verify` reports it.
--
-- user_id and agent_id deliberately have no foreign keys: the audit log
-- must keep its history after a user or agent is deleted. username keeps
-- the name as it was when the action happened.

CREATE TABLE audit_log (
    id            INTEGER PRIMARY KEY,
    ts            INTEGER NOT NULL,
    user_id       INTEGER,
    username      TEXT    NOT NULL,
    agent_id      INTEGER,
    action        TEXT    NOT NULL,
    details_json  TEXT    NOT NULL,
    outcome       TEXT    NOT NULL,
    ip            TEXT    NOT NULL,
    previous_hash TEXT    NOT NULL,
    hash          TEXT    NOT NULL
) STRICT;

CREATE TRIGGER audit_log_is_append_only_update
BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is append-only');
END;

CREATE TRIGGER audit_log_is_append_only_delete
BEFORE DELETE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log is append-only');
END;

// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Database is what the audit log needs from the store.
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
	Now() int64
}

// Log is the audit log.
type Log struct {
	database Database
}

// New returns the audit log stored in database.
func New(database Database) *Log {
	return &Log{database: database}
}

// Record appends entry to the log in its own transaction.
func (l *Log) Record(ctx context.Context, entry Entry) error {
	return l.database.Write(ctx, func(tx *sql.Tx) error {
		return l.RecordInTx(ctx, tx, entry)
	})
}

// RecordInTx appends entry inside tx, a transaction from store.Write, so the
// entry is saved if and only if the rest of the transaction is.
func (l *Log) RecordInTx(ctx context.Context, tx *sql.Tx, entry Entry) error {
	details, err := entry.validate()
	if err != nil {
		return err
	}

	previous, err := lastRecord(ctx, tx)
	if err != nil {
		return err
	}

	record := Record{
		ID:           previous.ID + 1,
		Time:         l.database.Now(),
		UserID:       entry.UserID,
		Username:     entry.Username,
		AgentID:      entry.AgentID,
		Action:       entry.Action,
		Details:      details,
		Outcome:      entry.Outcome,
		IP:           entry.IP,
		PreviousHash: previous.Hash,
	}
	record.Hash = computeHash(record)

	_, err = tx.ExecContext(ctx, `INSERT INTO audit_log
		(id, ts, user_id, username, agent_id, action, details_json, outcome, ip, previous_hash, hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.Time, record.UserID, record.Username, record.AgentID, record.Action,
		string(record.Details), string(record.Outcome), record.IP, record.PreviousHash, record.Hash)
	if err != nil {
		return fmt.Errorf("record audit entry %s: %w", entry.Action, err)
	}
	return nil
}

// lastRecord returns the newest row's ID and hash, or ID 0 and the genesis
// hash when the log is empty.
func lastRecord(ctx context.Context, tx *sql.Tx) (Record, error) {
	var last Record
	err := tx.QueryRowContext(ctx, `SELECT id, hash FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&last.ID, &last.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{ID: 0, Hash: genesisHash}, nil
	}
	if err != nil {
		return Record{}, fmt.Errorf("read last audit entry: %w", err)
	}
	return last, nil
}

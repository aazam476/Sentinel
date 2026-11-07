// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"database/sql"
	"fmt"
)

// MaxPageSize is the most entries List returns at once.
const MaxPageSize = 500

// ListOptions selects a page of entries, newest first.
type ListOptions struct {
	// BeforeID returns entries older than this ID. Zero starts at the newest.
	BeforeID int64
	// Limit is the page size, from 1 to MaxPageSize. Zero means MaxPageSize.
	Limit int
}

// List returns one page of entries, newest first. To get the next page, call
// it again with BeforeID set to the last entry's ID.
func (l *Log) List(ctx context.Context, options ListOptions) ([]Record, error) {
	limit := options.Limit
	if limit <= 0 || limit > MaxPageSize {
		limit = MaxPageSize
	}
	beforeID := options.BeforeID
	if beforeID <= 0 {
		beforeID = 1<<63 - 1
	}

	var records []Record
	err := l.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, selectRecords+` WHERE id < ? ORDER BY id DESC LIMIT ?`, beforeID, limit)
		if err != nil {
			return fmt.Errorf("list audit entries: %w", err)
		}
		records, err = scanRecords(rows)
		return err
	})
	return records, err
}

// All calls visit for every entry, oldest first, stopping at the first error.
// It reads in one transaction, so it sees a consistent log even while new
// entries are being added.
func (l *Log) All(ctx context.Context, visit func(Record) error) error {
	return l.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, selectRecords+` ORDER BY id ASC`)
		if err != nil {
			return fmt.Errorf("read audit log: %w", err)
		}
		defer func() { _ = rows.Close() }() // a close error after reading is not actionable

		for rows.Next() {
			record, err := scanRecord(rows)
			if err != nil {
				return err
			}
			if err := visit(record); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

const selectRecords = `SELECT id, ts, user_id, username, agent_id, action, details_json, outcome, ip,
	previous_hash, hash FROM audit_log`

func scanRecords(rows *sql.Rows) ([]Record, error) {
	defer func() { _ = rows.Close() }() // a close error after reading is not actionable

	records := []Record{}
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read audit entries: %w", err)
	}
	return records, nil
}

func scanRecord(rows *sql.Rows) (Record, error) {
	var record Record
	var details, outcome string
	err := rows.Scan(&record.ID, &record.Time, &record.UserID, &record.Username, &record.AgentID,
		&record.Action, &details, &outcome, &record.IP, &record.PreviousHash, &record.Hash)
	if err != nil {
		return Record{}, fmt.Errorf("read audit entry: %w", err)
	}
	record.Details = []byte(details)
	record.Outcome = Outcome(outcome)
	return record, nil
}

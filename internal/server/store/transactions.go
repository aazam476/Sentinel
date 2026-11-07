// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Read runs work inside a read-only transaction, so every query in it sees
// the same snapshot of the database. Many Reads can run at the same time.
func (s *Store) Read(ctx context.Context, work func(tx *sql.Tx) error) error {
	tx, err := s.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	// Rolling back a read-only transaction just releases the snapshot.
	defer func() { _ = tx.Rollback() }() // fails harmlessly after nothing changed

	return work(tx)
}

// Write runs work inside a read-write transaction on the writer goroutine
// and waits for it. The transaction commits if work returns nil and rolls
// back otherwise; work's error is returned unchanged so callers can check it
// with errors.Is.
func (s *Store) Write(ctx context.Context, work func(tx *sql.Tx) error) error {
	job := writeJob{ctx: ctx, work: work, result: make(chan error, 1)}

	select {
	case s.writes <- job:
	case <-s.stopped:
		return ErrClosed
	case <-ctx.Done():
		return fmt.Errorf("wait for database writer: %w", ctx.Err())
	}

	// Once the writer has the job it always sends a result, so waiting here
	// can't hang.
	return <-job.result
}

// runWriter executes write jobs one at a time until ctx is canceled. It is
// the only goroutine that writes to the database, which is how the
// one-writer rule of SQLite is kept. It runs from Open until Close.
func (s *Store) runWriter(ctx context.Context) {
	defer close(s.stopped)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.writes:
			job.result <- s.runWriteJob(job)
		}
	}
}

func (s *Store) runWriteJob(job writeJob) error {
	tx, err := s.writeDB.BeginTx(job.ctx, nil)
	if err != nil {
		return fmt.Errorf("begin write transaction: %w", err)
	}

	if err := job.work(tx); err != nil {
		return errors.Join(err, rollback(tx))
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit write transaction: %w", err)
	}
	return nil
}

// rollback undoes a failed transaction. It returns nil when the transaction
// was already finished, for example because its context was canceled.
func rollback(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("roll back write transaction: %w", err)
	}
	return nil
}

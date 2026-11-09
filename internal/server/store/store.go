// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	// Registers the pure-Go "sqlite" driver with database/sql. Importing a
	// driver for its side effect is how database/sql is designed to be used.
	_ "modernc.org/sqlite"
)

// Clock tells the current time.
type Clock interface {
	Now() time.Time
}

// ErrClosed is returned by Write after Close has been called. It is never
// reassigned; it exists so callers can check for it with errors.Is.
var ErrClosed = errors.New("store is closed")

// Store is an open database. Create it with Open and release it with Close.
type Store struct {
	readDB  *sql.DB
	writeDB *sql.DB
	clock   Clock
	writes  chan writeJob
	stopped chan struct{}
	stop    context.CancelFunc
}

// writeJob is one call to Write, waiting for the writer goroutine.
type writeJob struct {
	ctx    context.Context
	work   func(tx *sql.Tx) error
	result chan error
}

// Open opens (creating if needed) the SQLite database at path, applies
// pending migrations and starts the writer goroutine.
func Open(ctx context.Context, path string, clock Clock) (*Store, error) {
	writeDB, err := openConnection(ctx, path, false)
	if err != nil {
		return nil, err
	}
	// One connection: SQLite allows one writer, and the writer goroutine is
	// the only user of this handle.
	writeDB.SetMaxOpenConns(1)

	if err := migrate(ctx, writeDB, clock); err != nil {
		closeQuietly(writeDB)
		return nil, err
	}

	readDB, err := openConnection(ctx, path, true)
	if err != nil {
		closeQuietly(writeDB)
		return nil, err
	}

	writerCtx, stop := context.WithCancel(context.Background())
	store := &Store{
		readDB:  readDB,
		writeDB: writeDB,
		clock:   clock,
		writes:  make(chan writeJob),
		stopped: make(chan struct{}),
		stop:    stop,
	}
	go store.runWriter(writerCtx)
	return store, nil
}

// openConnection opens a database handle with the settings every connection
// needs. Read-only handles can't write even by mistake.
func openConnection(ctx context.Context, path string, readOnly bool) (*sql.DB, error) {
	query := url.Values{}
	// WAL lets readers keep reading while a write is in progress.
	query.Add("_pragma", "journal_mode(WAL)")
	// SQLite doesn't enforce foreign keys unless asked, per connection.
	query.Add("_pragma", "foreign_keys(ON)")
	// Wait up to 5 s for a lock instead of failing at once, e.g. while a
	// checkpoint runs.
	query.Add("_pragma", "busy_timeout(5000)")
	// With WAL, NORMAL is safe against corruption and much faster than FULL.
	query.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		query.Add("_pragma", "query_only(ON)")
	}

	db, err := sql.Open("sqlite", "file:"+escapeURIPath(path)+"?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		closeQuietly(db)
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	return db, nil
}

// Close stops the writer goroutine, waits for the write in progress to
// finish, and closes the database.
func (s *Store) Close() error {
	s.stop()
	<-s.stopped
	return errors.Join(s.readDB.Close(), s.writeDB.Close())
}

// Now returns the store's current time as UTC Unix milliseconds, the format
// every timestamp column uses.
func (s *Store) Now() int64 {
	return s.clock.Now().UnixMilli()
}

// escapeURIPath makes path safe inside a SQLite URI filename. SQLite decodes
// %XX escapes and treats "?" as the start of the options and "#" as the end
// of the name, so a data directory containing any of these would otherwise
// open the wrong file. https://www.sqlite.org/uri.html
func escapeURIPath(path string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
}

// closeQuietly closes db on an error path where the original error is the
// one worth reporting.
func closeQuietly(db *sql.DB) {
	_ = db.Close() // the caller is already returning a more useful error
}

// Snapshot writes a consistent copy of the database at path to dest, which
// must not exist. It works while the server runs: SQLite's VACUUM INTO
// reads one snapshot of the database, as any reader does under WAL.
func Snapshot(ctx context.Context, path, dest string) error {
	db, err := openConnection(ctx, path, false)
	if err != nil {
		return err
	}
	defer closeQuietly(db)
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("copy the database: %w", err)
	}
	return nil
}

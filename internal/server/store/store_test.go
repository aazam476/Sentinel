// SPDX-License-Identifier: Apache-2.0

package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func TestOpen(t *testing.T) {
	t.Run("creates the database and its tables", func(t *testing.T) {
		db := openStore(t, filepath.Join(t.TempDir(), "sentinel.db"))

		setSetting(t, db, "greeting", `"hello"`)

		if got := getSetting(t, db, "greeting"); got != `"hello"` {
			t.Errorf("setting = %q, want %q", got, `"hello"`)
		}
	})

	t.Run("keeps data and does not re-run migrations when reopened", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sentinel.db")
		first := openStore(t, path)
		setSetting(t, first, "kept", `1`)
		closeStore(t, first)

		second := openStore(t, path)

		if got := getSetting(t, second, "kept"); got != `1` {
			t.Errorf("setting after reopen = %q, want 1", got)
		}
		var appliedCount, distinctCount int
		mustRead(t, second, func(tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT COUNT(*), COUNT(DISTINCT version) FROM schema_migrations`).Scan(&appliedCount, &distinctCount)
		})
		if appliedCount != distinctCount {
			t.Errorf("schema_migrations has %d rows for %d versions; a migration ran twice", appliedCount, distinctCount)
		}
	})

	t.Run("returns an error when the folder does not exist", func(t *testing.T) {
		_, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "missing", "sentinel.db"), testClock())
		if err == nil {
			t.Error("Open returned nil for a path in a missing folder")
		}
	})
}

func TestWrite(t *testing.T) {
	t.Run("commits when the work succeeds", func(t *testing.T) {
		db := openStore(t, filepath.Join(t.TempDir(), "sentinel.db"))

		setSetting(t, db, "committed", `true`)

		if got := getSetting(t, db, "committed"); got != `true` {
			t.Errorf("setting = %q, want true", got)
		}
	})

	t.Run("rolls back and returns the work's error when it fails", func(t *testing.T) {
		db := openStore(t, filepath.Join(t.TempDir(), "sentinel.db"))
		workFailed := errors.New("work failed")

		err := db.Write(t.Context(), func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO settings (key, value_json) VALUES ('rolled-back', 'true')`); err != nil {
				return err
			}
			return workFailed
		})

		if !errors.Is(err, workFailed) {
			t.Errorf("Write error = %v, want the work's error", err)
		}
		if got := getSetting(t, db, "rolled-back"); got != "" {
			t.Errorf("setting = %q after a failed write, want it rolled back", got)
		}
	})

	t.Run("runs concurrent writes one at a time", func(t *testing.T) {
		db := openStore(t, filepath.Join(t.TempDir(), "sentinel.db"))
		setSetting(t, db, "counter", "0")
		const writers = 50

		var group sync.WaitGroup
		for range writers {
			group.Go(func() {
				if err := incrementCounter(t.Context(), db); err != nil {
					t.Errorf("increment: %v", err)
				}
			})
		}
		group.Wait()

		if got := getSetting(t, db, "counter"); got != strconv.Itoa(writers) {
			t.Errorf("counter = %s, want %d; writes overlapped and lost updates", got, writers)
		}
	})

	t.Run("returns ErrClosed after the store is closed", func(t *testing.T) {
		db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), testClock())
		if err != nil {
			t.Fatal(err)
		}
		closeStore(t, db)

		err = db.Write(t.Context(), func(tx *sql.Tx) error { return nil })

		if !errors.Is(err, store.ErrClosed) {
			t.Errorf("Write error = %v, want ErrClosed", err)
		}
	})

	t.Run("returns an error when the context is already canceled", func(t *testing.T) {
		db := openStore(t, filepath.Join(t.TempDir(), "sentinel.db"))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := db.Write(ctx, func(tx *sql.Tx) error { return nil })

		if !errors.Is(err, context.Canceled) {
			t.Errorf("Write error = %v, want context.Canceled", err)
		}
	})
}

func TestRead(t *testing.T) {
	t.Run("refuses to write inside a read transaction", func(t *testing.T) {
		db := openStore(t, filepath.Join(t.TempDir(), "sentinel.db"))

		err := db.Read(t.Context(), func(tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO settings (key, value_json) VALUES ('sneaky', '1')`)
			return err
		})

		if err == nil {
			t.Error("Read allowed an INSERT, want an error")
		}
	})

	t.Run("returns an error when the context is already canceled", func(t *testing.T) {
		db := openStore(t, filepath.Join(t.TempDir(), "sentinel.db"))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := db.Read(ctx, func(tx *sql.Tx) error { return nil })

		if err == nil {
			t.Error("Read returned nil with a canceled context")
		}
	})
}

func TestNow(t *testing.T) {
	t.Run("returns the clock's time in Unix milliseconds", func(t *testing.T) {
		clock := testClock()
		db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeStore(t, db) })

		if got, want := db.Now(), clock.Now().UnixMilli(); got != want {
			t.Errorf("Now() = %d, want %d", got, want)
		}
	})
}

func incrementCounter(ctx context.Context, db *store.Store) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		var value int
		if err := tx.QueryRowContext(ctx, `SELECT CAST(value_json AS INTEGER) FROM settings WHERE key = 'counter'`).Scan(&value); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE settings SET value_json = ? WHERE key = 'counter'`, strconv.Itoa(value+1))
		return err
	})
}

func testClock() *fakeclock.Clock {
	return fakeclock.New(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
}

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	db, err := store.Open(t.Context(), path, testClock())
	if err != nil {
		t.Fatalf("Open returned %v", err)
	}
	t.Cleanup(func() { _ = db.Close() }) // closing twice in a test that already closed is harmless
	return db
}

func closeStore(t *testing.T, db *store.Store) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}
}

func setSetting(t *testing.T, db *store.Store, key, value string) {
	t.Helper()
	err := db.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO settings (key, value_json) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json`, key, value)
		return err
	})
	if err != nil {
		t.Fatalf("set setting %s: %v", key, err)
	}
}

// getSetting returns a setting's value, or "" if it doesn't exist.
func getSetting(t *testing.T, db *store.Store, key string) string {
	t.Helper()
	var value string
	mustRead(t, db, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(t.Context(), `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	return value
}

func mustRead(t *testing.T, db *store.Store, work func(tx *sql.Tx) error) {
	t.Helper()
	if err := db.Read(t.Context(), work); err != nil {
		t.Fatalf("Read returned %v", err)
	}
}

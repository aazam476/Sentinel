// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func TestLoadMigrations(t *testing.T) {
	tests := []struct {
		name      string
		files     fstest.MapFS
		wantNames []string
		wantErr   string
	}{
		{
			name: "sorts migrations by number, not by name",
			files: fstest.MapFS{
				"migrations/0010_ten.sql": {Data: []byte("SELECT 10;")},
				"migrations/0002_two.sql": {Data: []byte("SELECT 2;")},
				"migrations/0001_one.sql": {Data: []byte("SELECT 1;")},
			},
			wantNames: []string{"0001_one.sql", "0002_two.sql", "0010_ten.sql"},
		},
		{
			name:      "returns nothing when there are no migrations",
			files:     fstest.MapFS{},
			wantNames: []string{},
		},
		{
			name:    "rejects a name without a number",
			files:   fstest.MapFS{"migrations/init.sql": {Data: []byte("SELECT 1;")}},
			wantErr: "must start with a positive number",
		},
		{
			name:    "rejects number zero",
			files:   fstest.MapFS{"migrations/0000_zero.sql": {Data: []byte("SELECT 1;")}},
			wantErr: "must start with a positive number",
		},
		{
			name: "rejects two migrations with the same number",
			files: fstest.MapFS{
				"migrations/0001_a.sql": {Data: []byte("SELECT 1;")},
				"migrations/0001_b.sql": {Data: []byte("SELECT 1;")},
			},
			wantErr: "same number",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			migrations, err := loadMigrations(test.files)

			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadMigrations returned %v", err)
			}
			names := make([]string, 0, len(migrations))
			for _, migration := range migrations {
				names = append(names, migration.name)
			}
			if strings.Join(names, ",") != strings.Join(test.wantNames, ",") {
				t.Errorf("names = %v, want %v", names, test.wantNames)
			}
		})
	}
}

func TestApplyMigration(t *testing.T) {
	t.Run("leaves no trace of a migration whose SQL fails", func(t *testing.T) {
		db := openTestConnection(t)
		broken := migration{version: 99, name: "0099_broken.sql", sql: "CREATE TABLE ok (id INTEGER); NOT VALID SQL;"}
		if err := migrate(t.Context(), db, testClock()); err != nil {
			t.Fatal(err)
		}

		err := applyMigration(t.Context(), db, broken, testClock())

		if err == nil || !strings.Contains(err.Error(), "0099_broken.sql") {
			t.Fatalf("error = %v, want one naming the migration", err)
		}
		var recorded int
		if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_migrations WHERE version = 99`).Scan(&recorded); err != nil {
			t.Fatal(err)
		}
		if recorded != 0 {
			t.Error("a failed migration was recorded as applied")
		}
	})

	t.Run("returns an error when the context is canceled", func(t *testing.T) {
		db := openTestConnection(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if err := migrate(ctx, db, testClock()); err == nil {
			t.Error("migrate returned nil with a canceled context")
		}
	})
}

func TestRollback(t *testing.T) {
	t.Run("ignores a transaction that is already finished", func(t *testing.T) {
		db := openTestConnection(t)
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}

		if err := rollback(tx); err != nil {
			t.Errorf("rollback after commit returned %v, want nil", err)
		}
	})
}

func TestRunWriteJob(t *testing.T) {
	t.Run("returns an error when the job's context is already canceled", func(t *testing.T) {
		store, err := Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), testClock())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() }) // only the error below matters
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err = store.runWriteJob(writeJob{ctx: ctx, work: func(tx *sql.Tx) error { return nil }})

		if err == nil {
			t.Error("runWriteJob returned nil with a canceled context")
		}
	})

	t.Run("reports a commit that fails", func(t *testing.T) {
		store, err := Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), testClock())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() }) // only the error below matters

		// Finishing the transaction inside the work makes the later commit fail.
		err = store.runWriteJob(writeJob{ctx: t.Context(), work: func(tx *sql.Tx) error { return tx.Rollback() }})

		if err == nil || !strings.Contains(err.Error(), "commit") {
			t.Errorf("runWriteJob error = %v, want a commit error", err)
		}
	})
}

func openTestConnection(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openConnection(t.Context(), filepath.Join(t.TempDir(), "test.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() }) // nothing to check when a test connection closes
	return db
}

func testClock() *fakeclock.Clock {
	return fakeclock.New(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
}

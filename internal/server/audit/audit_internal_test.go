// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	// Registers the "sqlite" driver for the hand-built test databases below.
	_ "modernc.org/sqlite"
)

func TestSpreadsheetSafe(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "leaves ordinary text alone", value: "alice", want: "alice"},
		{name: "leaves an empty value alone", value: "", want: ""},
		{name: "neutralizes a formula", value: "=1+1", want: "'=1+1"},
		{name: "neutralizes a leading plus", value: "+1", want: "'+1"},
		{name: "neutralizes a leading minus", value: "-1", want: "'-1"},
		{name: "neutralizes a leading at sign", value: "@SUM(A1)", want: "'@SUM(A1)"},
		{name: "neutralizes a leading tab", value: "\t=1", want: "'\t=1"},
		{name: "leaves a formula character later in the value alone", value: "a=b", want: "a=b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := spreadsheetSafe(test.value); got != test.want {
				t.Errorf("spreadsheetSafe(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestOptionalID(t *testing.T) {
	id := int64(42)
	if got := optionalID(&id); got != "42" {
		t.Errorf("optionalID(42) = %q, want 42", got)
	}
	if got := optionalID(nil); got != "" {
		t.Errorf("optionalID(nil) = %q, want empty", got)
	}
}

func TestDatabaseErrors(t *testing.T) {
	failing := New(failingDatabase{})

	t.Run("List reports a database error", func(t *testing.T) {
		if _, err := failing.List(t.Context(), ListOptions{}); err == nil {
			t.Error("List returned nil")
		}
	})

	t.Run("Verify reports a database error", func(t *testing.T) {
		if _, err := failing.Verify(t.Context()); err == nil {
			t.Error("Verify returned nil")
		}
	})

	t.Run("Export reports a database error", func(t *testing.T) {
		for _, format := range []Format{FormatCSV, FormatJSON} {
			if err := failing.Export(t.Context(), &bytes.Buffer{}, format); err == nil {
				t.Errorf("Export(%s) returned nil", format)
			}
		}
	})
}

func TestBrokenTable(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		run    func(t *testing.T, log *Log) error
	}{
		{
			name:   "Record reports a missing table",
			schema: `CREATE TABLE unrelated (id INTEGER)`,
			run: func(t *testing.T, log *Log) error {
				return log.Record(t.Context(), Entry{Action: "x", Outcome: OutcomeSuccess})
			},
		},
		{
			name:   "Record reports an insert the table refuses",
			schema: `CREATE TABLE audit_log (id INTEGER PRIMARY KEY, hash TEXT)`,
			run: func(t *testing.T, log *Log) error {
				return log.Record(t.Context(), Entry{Action: "x", Outcome: OutcomeSuccess})
			},
		},
		{
			name:   "List reports a missing table",
			schema: `CREATE TABLE unrelated (id INTEGER)`,
			run: func(t *testing.T, log *Log) error {
				_, err := log.List(t.Context(), ListOptions{})
				return err
			},
		},
		{
			name:   "List reports a row that can't be read",
			schema: brokenRowSchema,
			run: func(t *testing.T, log *Log) error {
				_, err := log.List(t.Context(), ListOptions{})
				return err
			},
		},
		{
			name:   "Verify reports a row that can't be read",
			schema: brokenRowSchema,
			run: func(t *testing.T, log *Log) error {
				_, err := log.Verify(t.Context())
				return err
			},
		},
		{
			name:   "Verify reports a missing table",
			schema: `CREATE TABLE unrelated (id INTEGER)`,
			run: func(t *testing.T, log *Log) error {
				_, err := log.Verify(t.Context())
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := New(newRawDatabase(t, test.schema))

			if err := test.run(t, log); err == nil {
				t.Error("returned nil, want an error")
			}
		})
	}
}

func TestExportWriteErrors(t *testing.T) {
	// The CSV writer buffers everything into one write when it flushes; the
	// JSON export writes the opening bracket, separator, entry and closing
	// bracket separately. A failure at each of those writes must surface.
	writesBeforeEnd := map[Format][]int{FormatCSV: {0}, FormatJSON: {0, 1, 2, 3}}
	for format, failures := range writesBeforeEnd {
		for _, failAfter := range failures {
			t.Run(fmt.Sprintf("%s reports a write failing after %d writes", format, failAfter), func(t *testing.T) {
				log := New(newRawDatabase(t, ""))
				if err := log.Record(t.Context(), Entry{Action: "x", Outcome: OutcomeSuccess}); err != nil {
					t.Fatal(err)
				}

				err := log.Export(t.Context(), &failingWriter{allowedWrites: failAfter}, format)

				if err == nil {
					t.Errorf("Export with a writer failing after %d writes returned nil", failAfter)
				}
			})
		}
	}
}

// brokenRowSchema creates an audit_log whose only row has a text timestamp,
// which can't be read into an integer.
const brokenRowSchema = `CREATE TABLE audit_log (id INTEGER PRIMARY KEY, ts, user_id, username, agent_id, action,
	details_json, outcome, ip, previous_hash, hash);
INSERT INTO audit_log VALUES (1, 'not a number', NULL, '', NULL, 'x', '{}', 'success', '', '', '')`

// failingDatabase fails every transaction.
type failingDatabase struct{}

func (failingDatabase) Read(context.Context, func(*sql.Tx) error) error {
	return errors.New("database unavailable")
}

func (failingDatabase) Write(context.Context, func(*sql.Tx) error) error {
	return errors.New("database unavailable")
}

func (failingDatabase) Now() int64 { return 0 }

// rawDatabase runs transactions on a plain SQLite file whose schema the test
// controls, to reach error paths a correct schema never hits.
type rawDatabase struct {
	db *sql.DB
}

func newRawDatabase(t *testing.T, schema string) rawDatabase {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "raw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() }) // nothing to check when a test database closes
	if schema == "" {
		schema = realSchema(t)
	}
	if _, err := db.ExecContext(t.Context(), schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return rawDatabase{db: db}
}

// realSchema returns the audit_log table from the real migration.
func realSchema(t *testing.T) string {
	t.Helper()
	return `CREATE TABLE audit_log (id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, user_id INTEGER,
		username TEXT NOT NULL, agent_id INTEGER, action TEXT NOT NULL, details_json TEXT NOT NULL,
		outcome TEXT NOT NULL, ip TEXT NOT NULL, previous_hash TEXT NOT NULL, hash TEXT NOT NULL)`
}

func (r rawDatabase) Read(ctx context.Context, work func(*sql.Tx) error) error {
	return r.run(ctx, work)
}

func (r rawDatabase) Write(ctx context.Context, work func(*sql.Tx) error) error {
	return r.run(ctx, work)
}

func (r rawDatabase) run(ctx context.Context, work func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := work(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (rawDatabase) Now() int64 { return 0 }

// failingWriter accepts a number of writes and then fails every one.
type failingWriter struct {
	allowedWrites int
}

func (w *failingWriter) Write(data []byte) (int, error) {
	if w.allowedWrites <= 0 {
		return 0, errors.New("disk full")
	}
	w.allowedWrites--
	return len(data), nil
}

func TestCheckLink(t *testing.T) {
	first := Record{ID: 1, Details: []byte("{}"), Outcome: OutcomeSuccess, PreviousHash: genesisHash}
	first.Hash = computeHash(first)

	if problem := checkLink(Record{ID: 0, Hash: genesisHash}, first); problem != "" {
		t.Errorf("checkLink on a correct first entry = %q, want no problem", problem)
	}
	if problem := checkLink(Record{ID: 5, Hash: genesisHash}, first); !strings.Contains(problem, "out of order") {
		t.Errorf("checkLink with a gap = %q, want an ordering problem", problem)
	}
}

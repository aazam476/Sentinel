// SPDX-License-Identifier: Apache-2.0

package audit_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func TestSEC40_EntriesFormAHashChain(t *testing.T) {
	log, _ := newLog(t)
	userID := int64(7)
	recordAll(t, log,
		audit.Entry{UserID: &userID, Username: "alice", Action: "auth.login", Outcome: audit.OutcomeSuccess, IP: "203.0.113.7"},
		audit.Entry{Username: "mallory", Action: "auth.login", Outcome: audit.OutcomeFailure, IP: "198.51.100.1",
			Details: map[string]any{"reason": "wrong_password"}},
		audit.Entry{UserID: &userID, Username: "alice", Action: "auth.logout", Outcome: audit.OutcomeSuccess, IP: "203.0.113.7"},
	)

	records, err := log.List(t.Context(), audit.ListOptions{})
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	t.Run("returns entries newest first with sequential IDs", func(t *testing.T) {
		if len(records) != 3 || records[0].ID != 3 || records[2].ID != 1 {
			t.Fatalf("records = %+v, want IDs 3, 2, 1", records)
		}
	})

	t.Run("starts the chain from an all-zero hash", func(t *testing.T) {
		if records[2].PreviousHash != strings.Repeat("0", 64) {
			t.Errorf("first previous hash = %q, want 64 zeros", records[2].PreviousHash)
		}
	})

	t.Run("links every entry to the one before it", func(t *testing.T) {
		if records[1].PreviousHash != records[2].Hash || records[0].PreviousHash != records[1].Hash {
			t.Error("an entry's previous hash does not match the earlier entry's hash")
		}
	})

	t.Run("stores who, what, when, where and how it ended", func(t *testing.T) {
		failed := records[1]
		if failed.Username != "mallory" || failed.UserID != nil || failed.Action != "auth.login" ||
			failed.Outcome != audit.OutcomeFailure || failed.IP != "198.51.100.1" {
			t.Errorf("record = %+v, want the failed login as recorded", failed)
		}
		if string(failed.Details) != `{"reason":"wrong_password"}` {
			t.Errorf("details = %s, want the reason", failed.Details)
		}
		if failed.Time != testStart().UnixMilli() {
			t.Errorf("time = %d, want the clock's time", failed.Time)
		}
	})

	t.Run("stores empty details as an empty object", func(t *testing.T) {
		if string(records[0].Details) != "{}" {
			t.Errorf("details = %s, want {}", records[0].Details)
		}
	})
}

func TestSEC40_AuditLogIsAppendOnly(t *testing.T) {
	log, database := newLog(t)
	recordAll(t, log, audit.Entry{Action: "auth.login", Outcome: audit.OutcomeSuccess})

	tests := []struct {
		name      string
		statement string
	}{
		{name: "refuses to change an entry", statement: `UPDATE audit_log SET outcome = 'failure'`},
		{name: "refuses to delete an entry", statement: `DELETE FROM audit_log`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := database.Write(t.Context(), func(tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), test.statement)
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Errorf("error = %v, want an append-only refusal", err)
			}
		})
	}
}

func TestSEC41_VerifyDetectsTampering(t *testing.T) {
	tests := []struct {
		name         string
		tamper       string
		wantBrokenAt int64
		wantProblem  string
	}{
		{
			name:         "finds a changed entry",
			tamper:       `UPDATE audit_log SET outcome = 'success' WHERE id = 2`,
			wantBrokenAt: 2,
			wantProblem:  "changed",
		},
		{
			name:         "finds a removed entry",
			tamper:       `DELETE FROM audit_log WHERE id = 2`,
			wantBrokenAt: 3,
			wantProblem:  "missing",
		},
		{
			name:         "finds an entry whose stored hash was replaced",
			tamper:       `UPDATE audit_log SET hash = 'forged' WHERE id = 2`,
			wantBrokenAt: 2,
			wantProblem:  "changed",
		},
		{
			name:         "finds an entry whose link to the previous one was changed",
			tamper:       `UPDATE audit_log SET previous_hash = 'forged' WHERE id = 3`,
			wantBrokenAt: 3,
			wantProblem:  "does not link",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sentinel.db")
			log := newLogAt(t, path)
			recordThree(t, log)

			tamperDirectly(t, path, test.tamper)

			result, err := log.Verify(t.Context())
			if err != nil {
				t.Fatalf("Verify returned %v", err)
			}
			if result.Valid || result.BrokenAtID != test.wantBrokenAt || !strings.Contains(result.Problem, test.wantProblem) {
				t.Errorf("Verify = %+v, want broken at %d with a problem mentioning %q", result, test.wantBrokenAt, test.wantProblem)
			}
		})
	}
}

func TestSEC41_VerifyAcceptsAnUntouchedLog(t *testing.T) {
	tests := []struct {
		name    string
		entries int
	}{
		{name: "accepts an empty log", entries: 0},
		{name: "accepts a log with entries", entries: 3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log, _ := newLog(t)
			for range test.entries {
				recordAll(t, log, audit.Entry{Action: "auth.login", Outcome: audit.OutcomeSuccess})
			}

			result, err := log.Verify(t.Context())

			if err != nil || !result.Valid || result.Checked != int64(test.entries) {
				t.Errorf("Verify = %+v, %v; want valid with %d checked", result, err, test.entries)
			}
		})
	}
}

func TestSEC41_AuditLogExports(t *testing.T) {
	log, _ := newLog(t)
	recordAll(t, log,
		audit.Entry{Username: "=HYPERLINK(\"http://evil\")", Action: "auth.login", Outcome: audit.OutcomeFailure, IP: "203.0.113.7"},
		audit.Entry{Username: "alice", Action: "auth.logout", Outcome: audit.OutcomeSuccess, IP: "203.0.113.7"},
	)

	t.Run("exports CSV with a header and one row per entry, oldest first", func(t *testing.T) {
		rows := exportCSV(t, log)
		if len(rows) != 3 || rows[0][0] != "id" || rows[1][0] != "1" || rows[2][0] != "2" {
			t.Fatalf("rows = %v, want a header and entries 1 and 2", rows)
		}
		if rows[1][1] != "2026-11-07T12:00:00Z" {
			t.Errorf("time = %q, want UTC RFC 3339", rows[1][1])
		}
	})

	t.Run("stops a CSV value from running as a spreadsheet formula", func(t *testing.T) {
		rows := exportCSV(t, log)
		if !strings.HasPrefix(rows[1][3], "'=") {
			t.Errorf("username = %q, want it prefixed with an apostrophe", rows[1][3])
		}
	})

	t.Run("exports JSON as an array of entries, oldest first", func(t *testing.T) {
		var output bytes.Buffer
		if err := log.Export(t.Context(), &output, audit.FormatJSON); err != nil {
			t.Fatalf("Export returned %v", err)
		}
		var records []audit.Record
		if err := json.Unmarshal(output.Bytes(), &records); err != nil {
			t.Fatalf("JSON export does not parse: %v\n%s", err, output.String())
		}
		if len(records) != 2 || records[0].ID != 1 || records[1].Username != "alice" {
			t.Errorf("records = %+v, want entries 1 and 2", records)
		}
	})

	t.Run("exports an empty log as an empty JSON array", func(t *testing.T) {
		empty, _ := newLog(t)
		var output bytes.Buffer
		if err := empty.Export(t.Context(), &output, audit.FormatJSON); err != nil {
			t.Fatal(err)
		}
		var records []audit.Record
		if err := json.Unmarshal(output.Bytes(), &records); err != nil || len(records) != 0 {
			t.Errorf("empty export = %q, want []", output.String())
		}
	})

	t.Run("rejects an unknown format", func(t *testing.T) {
		if err := log.Export(t.Context(), &bytes.Buffer{}, "xml"); err == nil {
			t.Error("Export accepted format xml")
		}
	})
}

func TestRecord(t *testing.T) {
	tests := []struct {
		name    string
		entry   audit.Entry
		wantErr string
	}{
		{name: "rejects an entry without an action", entry: audit.Entry{Outcome: audit.OutcomeSuccess}, wantErr: "no action"},
		{name: "rejects an unknown outcome", entry: audit.Entry{Action: "x", Outcome: "maybe"}, wantErr: "unknown outcome"},
		{name: "rejects details that can't be stored as JSON", entry: audit.Entry{Action: "x", Outcome: audit.OutcomeSuccess,
			Details: map[string]any{"bad": make(chan int)}}, wantErr: "encode details"},
		{name: "accepts every known outcome", entry: audit.Entry{Action: "x", Outcome: audit.OutcomeStarted}},
		{name: "accepts denied", entry: audit.Entry{Action: "x", Outcome: audit.OutcomeDenied}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log, _ := newLog(t)
			err := log.Record(t.Context(), test.entry)
			if test.wantErr == "" {
				if err != nil {
					t.Errorf("Record returned %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("Record error = %v, want one containing %q", err, test.wantErr)
			}
		})
	}
}

func TestRecordInTx(t *testing.T) {
	t.Run("drops the entry when the surrounding transaction fails", func(t *testing.T) {
		log, database := newLog(t)

		_ = database.Write(t.Context(), func(tx *sql.Tx) error { // the error is the point of this test
			if err := log.RecordInTx(t.Context(), tx, audit.Entry{Action: "x", Outcome: audit.OutcomeSuccess}); err != nil {
				return err
			}
			return context.Canceled
		})

		records, err := log.List(t.Context(), audit.ListOptions{})
		if err != nil || len(records) != 0 {
			t.Errorf("records = %v, %v; want none after a rolled-back transaction", records, err)
		}
	})

	t.Run("keeps the chain valid under concurrent writers", func(t *testing.T) {
		log, _ := newLog(t)
		var group sync.WaitGroup
		for range 20 {
			group.Go(func() {
				if err := log.Record(t.Context(), audit.Entry{Action: "x", Outcome: audit.OutcomeSuccess}); err != nil {
					t.Errorf("Record: %v", err)
				}
			})
		}
		group.Wait()

		result, err := log.Verify(t.Context())
		if err != nil || !result.Valid || result.Checked != 20 {
			t.Errorf("Verify = %+v, %v; want 20 valid entries", result, err)
		}
	})
}

func TestList(t *testing.T) {
	log, _ := newLog(t)
	for range 5 {
		recordAll(t, log, audit.Entry{Action: "x", Outcome: audit.OutcomeSuccess})
	}

	tests := []struct {
		name    string
		options audit.ListOptions
		wantIDs []int64
	}{
		{name: "returns everything newest first by default", options: audit.ListOptions{}, wantIDs: []int64{5, 4, 3, 2, 1}},
		{name: "limits the page size", options: audit.ListOptions{Limit: 2}, wantIDs: []int64{5, 4}},
		{name: "continues from a cursor", options: audit.ListOptions{BeforeID: 4, Limit: 2}, wantIDs: []int64{3, 2}},
		{name: "returns an empty page past the oldest entry", options: audit.ListOptions{BeforeID: 1}, wantIDs: []int64{}},
		{name: "treats a too-large limit as the maximum", options: audit.ListOptions{Limit: 10_000}, wantIDs: []int64{5, 4, 3, 2, 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			records, err := log.List(t.Context(), test.options)
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, 0, len(records))
			for _, record := range records {
				ids = append(ids, record.ID)
			}
			if !equalIDs(ids, test.wantIDs) {
				t.Errorf("IDs = %v, want %v", ids, test.wantIDs)
			}
		})
	}
}

func testStart() time.Time {
	return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
}

func newLog(t *testing.T) (*audit.Log, *store.Store) {
	t.Helper()
	database := openStore(t, filepath.Join(t.TempDir(), "sentinel.db"))
	return audit.New(database), database
}

func newLogAt(t *testing.T, path string) *audit.Log {
	t.Helper()
	return audit.New(openStore(t, path))
}

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	database, err := store.Open(t.Context(), path, fakeclock.New(testStart()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() }) // nothing to check when a test store closes
	return database
}

func recordAll(t *testing.T, log *audit.Log, entries ...audit.Entry) {
	t.Helper()
	for _, entry := range entries {
		if err := log.Record(t.Context(), entry); err != nil {
			t.Fatalf("Record(%s) returned %v", entry.Action, err)
		}
	}
}

func recordThree(t *testing.T, log *audit.Log) {
	t.Helper()
	recordAll(t, log,
		audit.Entry{Username: "alice", Action: "auth.login", Outcome: audit.OutcomeSuccess},
		audit.Entry{Username: "alice", Action: "user.role.change", Outcome: audit.OutcomeFailure},
		audit.Entry{Username: "alice", Action: "auth.logout", Outcome: audit.OutcomeSuccess},
	)
}

// tamperDirectly edits the database file the way an attacker with file
// access would: through a separate connection, after removing the triggers
// that protect the table.
func tamperDirectly(t *testing.T, path, statement string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }() // nothing to check when the tampering connection closes
	for _, step := range []string{
		`DROP TRIGGER audit_log_is_append_only_update`,
		`DROP TRIGGER audit_log_is_append_only_delete`,
		statement,
	} {
		if _, err := db.ExecContext(t.Context(), step); err != nil {
			t.Fatalf("tamper %q: %v", step, err)
		}
	}
}

func exportCSV(t *testing.T, log *audit.Log) [][]string {
	t.Helper()
	var output bytes.Buffer
	if err := log.Export(t.Context(), &output, audit.FormatCSV); err != nil {
		t.Fatalf("Export returned %v", err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("CSV export does not parse: %v", err)
	}
	return rows
}

func equalIDs(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

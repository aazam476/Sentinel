// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/clitest"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func TestSEC41_AuditCommands(t *testing.T) {
	binary := clitest.Build(t, serverPackage)
	missingConfig := filepath.Join(t.TempDir(), "missing.toml")

	t.Run("verify reports an intact log", func(t *testing.T) {
		dataDir := seedAuditLog(t)
		result := clitest.Run(t, binary, "audit", "verify", "--config", missingConfig, "--data-dir", dataDir)
		if result.ExitCode != 0 || !strings.Contains(result.Stdout, "intact: 2 entries") {
			t.Errorf("verify = %d %q %q, want exit 0 and 2 entries intact", result.ExitCode, result.Stdout, result.Stderr)
		}
	})

	t.Run("verify reports tampering and exits with failure", func(t *testing.T) {
		dataDir := seedAuditLog(t)
		tamper(t, filepath.Join(dataDir, "sentinel.db"))

		result := clitest.Run(t, binary, "audit", "verify", "--config", missingConfig, "--data-dir", dataDir)

		if result.ExitCode != 1 || !strings.Contains(result.Stdout, "NOT intact") {
			t.Errorf("verify = %d %q, want exit 1 and NOT intact", result.ExitCode, result.Stdout)
		}
	})

	t.Run("verify refuses to create a database that doesn't exist", func(t *testing.T) {
		result := clitest.Run(t, binary, "audit", "verify", "--config", missingConfig, "--data-dir", t.TempDir())
		if result.ExitCode != 1 || !strings.Contains(result.Stderr, "no database") {
			t.Errorf("verify = %d %q, want exit 1 and 'no database'", result.ExitCode, result.Stderr)
		}
	})

	t.Run("export writes CSV", func(t *testing.T) {
		dataDir := seedAuditLog(t)
		result := clitest.Run(t, binary, "audit", "export", "--format", "csv", "--config", missingConfig, "--data-dir", dataDir)
		if result.ExitCode != 0 || !strings.HasPrefix(result.Stdout, "id,time,") || strings.Count(result.Stdout, "\n") != 3 {
			t.Errorf("export = %d %q, want a header and two rows", result.ExitCode, result.Stdout)
		}
	})

	t.Run("export refuses an unknown format", func(t *testing.T) {
		dataDir := seedAuditLog(t)
		result := clitest.Run(t, binary, "audit", "export", "--format", "xml", "--config", missingConfig, "--data-dir", dataDir)
		if result.ExitCode != 1 {
			t.Errorf("export --format xml exit code = %d, want 1", result.ExitCode)
		}
	})

	t.Run("an unknown audit subcommand is a usage error", func(t *testing.T) {
		result := clitest.Run(t, binary, "audit", "shred")
		if result.ExitCode != 2 || !strings.Contains(result.Stderr, "Usage") {
			t.Errorf("audit shred = %d %q, want exit 2 with usage", result.ExitCode, result.Stderr)
		}
	})

	t.Run("an unknown audit flag is a usage error", func(t *testing.T) {
		result := clitest.Run(t, binary, "audit", "verify", "--shred")
		if result.ExitCode != 2 {
			t.Errorf("audit verify --shred exit code = %d, want 2", result.ExitCode)
		}
	})
}

// seedAuditLog creates a data directory with a database holding two audit
// entries.
func seedAuditLog(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	database, err := store.Open(t.Context(), filepath.Join(dataDir, "sentinel.db"),
		fakeclock.New(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }() // the CLI reopens the file afterwards
	log := audit.New(database)
	for _, action := range []string{"auth.login", "auth.logout"} {
		if err := log.Record(t.Context(), audit.Entry{Username: "alice", Action: action, Outcome: audit.OutcomeSuccess}); err != nil {
			t.Fatal(err)
		}
	}
	return dataDir
}

// tamper changes an audit entry the way someone with file access could.
func tamper(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }() // nothing to check when the tampering connection closes
	for _, statement := range []string{
		`DROP TRIGGER audit_log_is_append_only_update`,
		`UPDATE audit_log SET username = 'mallory' WHERE id = 1`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

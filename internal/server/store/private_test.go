// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package store_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheDatabaseIsForItsOwnerAlone(t *testing.T) {
	// A data directory the server didn't make, readable by everyone: a
	// mounted volume, or a folder made by hand.
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o755); err != nil { //nolint:gosec // G302: the open folder this test is about
		t.Fatal(err)
	}
	path := filepath.Join(directory, "sentinel.db")
	db := openStore(t, path)
	setSetting(t, db, "written", `1`) // so the write-ahead log exists too

	found := 0
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(file)
		if err != nil {
			continue // SQLite hasn't needed this one yet
		}
		found++
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s has mode %04o, want 0600: it holds password hashes and the audit log", filepath.Base(file), mode)
		}
	}
	if found < 2 {
		t.Errorf("only %d of the database's files exist; the test checked too little", found)
	}

	t.Run("and one made readable by everyone is made private again when it is opened", func(t *testing.T) {
		closeStore(t, db)
		if err := os.Chmod(path, 0o644); err != nil { //nolint:gosec // G302: the open file this test is about
			t.Fatal(err)
		}
		openStore(t, path)
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("the reopened database has mode %v (%v), want 0600", info.Mode().Perm(), err)
		}
	})
}

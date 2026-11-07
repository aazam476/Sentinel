// SPDX-License-Identifier: Apache-2.0

//go:build windows

package localaccess

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSEC08_OnlyAccountsThatCanWriteTheDatabaseMayResetTheAdmin(t *testing.T) {
	t.Run("allows an account that can write the database", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dataDir, "sentinel.db"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Check(dataDir); err != nil {
			t.Errorf("Check = %v", err)
		}
	})

	t.Run("reports a missing data directory", func(t *testing.T) {
		if err := Check(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Error("Check returned nil")
		}
	})

	t.Run("reports a missing database", func(t *testing.T) {
		if err := Check(t.TempDir()); err == nil || errors.Is(err, ErrNotAllowed) {
			t.Errorf("Check = %v, want a missing-database error", err)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package identity_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSEC20_OnlyTheAgentsUserCanUseTheStateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o755); err != nil { //nolint:gosec // G301: as if an installer left it readable
		t.Fatal(err)
	}
	store := openStore(t, dir)
	if _, err := store.Key(); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(validState(t)); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		dir:                              0o700,
		filepath.Join(dir, "agent-key"):  0o600,
		filepath.Join(dir, "state.json"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %v, want %v", path, got, want)
		}
	}
}

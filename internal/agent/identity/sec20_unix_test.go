// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package identity_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/identity"
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

func TestSEC20_AStateDirectoryMadeBySomeoneElseIsTakenOver(t *testing.T) {
	t.Run("refuses a link instead of following it", func(t *testing.T) {
		base := t.TempDir()
		elsewhere := filepath.Join(base, "elsewhere")
		if err := os.Mkdir(elsewhere, 0o755); err != nil { //nolint:gosec // G301: somebody else's folder
			t.Fatal(err)
		}
		dir := filepath.Join(base, "state")
		if err := os.Symlink(elsewhere, dir); err != nil {
			t.Fatal(err)
		}
		if _, err := identity.Open(dir); err == nil {
			t.Error("Open followed a link")
		}
		if info, err := os.Stat(elsewhere); err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("the link's target was changed: %v, %v", info.Mode(), err)
		}
	})

	t.Run("tightens files someone left in it, and refuses links inside", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(dir, 0o777); err != nil { //nolint:gosec // G301: as an attacker would leave it
			t.Fatal(err)
		}
		planted := filepath.Join(dir, "agent-key")
		if err := os.WriteFile(planted, []byte("x"), 0o666); err != nil { //nolint:gosec // G306: as an attacker would leave it
			t.Fatal(err)
		}
		if _, err := identity.Open(dir); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Stat(planted); info.Mode().Perm() != 0o600 {
			t.Errorf("planted file mode = %v, want 0600", info.Mode().Perm())
		}
		if err := os.Symlink("/etc/passwd", filepath.Join(dir, "state.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := identity.Open(dir); err == nil {
			t.Error("Open accepted a link inside the state directory")
		}
	})

	t.Run("takes ownership of a directory another user made", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("changing owners needs root")
		}
		dir := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(dir, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		if _, err := identity.Open(dir); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if stat := info.Sys().(*syscall.Stat_t); stat.Uid != 0 {
			t.Errorf("owner uid = %d, want 0", stat.Uid)
		}
	})
}

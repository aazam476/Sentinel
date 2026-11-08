// SPDX-License-Identifier: Apache-2.0

package keyfile_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/shared/keyfile"
)

func TestLoadOrCreate(t *testing.T) {
	t.Run("creates the file on first use and reads it back afterwards", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "key")
		first, err := keyfile.LoadOrCreate(path, "test key", func() []byte { return []byte("first") })
		if err != nil {
			t.Fatal(err)
		}
		second, err := keyfile.LoadOrCreate(path, "test key", func() []byte { return []byte("second") })
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, []byte("first")) || !bytes.Equal(second, first) {
			t.Errorf("got %q then %q, want the first key both times", first, second)
		}
	})

	t.Run("creates the file readable only by its owner", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows has no Unix file modes; the directory's access list protects the key")
		}
		path := filepath.Join(t.TempDir(), "key")
		if _, err := keyfile.LoadOrCreate(path, "test key", func() []byte { return []byte("k") }); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", info.Mode().Perm())
		}
	})

	t.Run("refuses a key other users can read, saying how to fix it", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows has no Unix file modes")
		}
		path := filepath.Join(t.TempDir(), "key")
		if err := os.WriteFile(path, []byte("k"), 0o644); err != nil { //nolint:gosec // G306: the test needs unsafe permissions
			t.Fatal(err)
		}
		_, err := keyfile.LoadOrCreate(path, "test key", func() []byte { return nil })
		if err == nil || !strings.Contains(err.Error(), "chmod 600") {
			t.Errorf("err = %v, want advice to chmod 600", err)
		}
	})

	t.Run("reports a key that can't be read", func(t *testing.T) {
		// A directory, not a file, with safe permissions so the read is what fails.
		path := filepath.Join(t.TempDir(), "key")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := keyfile.LoadOrCreate(path, "test key", func() []byte { return nil }); err == nil {
			t.Error("LoadOrCreate returned nil for a directory")
		}
	})

	t.Run("reports a key that can't be created", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing", "key")
		if _, err := keyfile.LoadOrCreate(path, "test key", func() []byte { return nil }); err == nil {
			t.Error("LoadOrCreate returned nil for a missing directory")
		}
	})
}

func TestWrite(t *testing.T) {
	t.Run("never overwrites an existing key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "key")
		if err := keyfile.Write(path, "test key", []byte("a")); err != nil {
			t.Fatal(err)
		}
		if err := keyfile.Write(path, "test key", []byte("b")); err == nil {
			t.Error("second Write returned nil")
		}
	})
}

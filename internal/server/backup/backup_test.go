// SPDX-License-Identifier: Apache-2.0

package backup_test

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/backup"
)

const passphrase = "correct horse battery staple"

// dataDir makes a data directory with a database, keys, a TLS folder, and
// the files a backup leaves out.
func dataDir(t *testing.T) backup.Contents {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"sentinel.db":            "live database",
		"sentinel.db-wal":        "journal",
		"setup-token":            "token",
		"secret.key":             "0123456789abcdef0123456789abcdef",
		"server-key":             "seed",
		"tls/self-signed.crt":    "certificate",
		"tls/acme/example.com":   "acme",
		"backup-snapshot.db.tmp": "snapshot in progress",
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	config := filepath.Join(t.TempDir(), "server.toml")
	for path, content := range map[string]string{snapshot: "consistent copy", config: "listen = \":8443\"\n"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return backup.Contents{DataDir: dir, Database: snapshot, Config: config}
}

func written(t *testing.T, contents backup.Contents) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := backup.Write(&out, []byte(passphrase), contents); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func read(file []byte, phrase string) (map[string]string, error) {
	got := map[string]string{}
	err := backup.Read(bytes.NewReader(file), []byte(phrase), func(name string, data io.Reader) error {
		content, err := io.ReadAll(data)
		got[name] = string(content)
		return err
	})
	return got, err
}

func TestRoundTrip(t *testing.T) {
	t.Run("keeps the database copy, keys, certificates and config, and leaves out the rest", func(t *testing.T) {
		contents := dataDir(t)
		got, err := read(written(t, contents), passphrase)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"sentinel.db": "consistent copy", "secret.key": "0123456789abcdef0123456789abcdef", "server-key": "seed",
			"tls/self-signed.crt": "certificate", "tls/acme/example.com": "acme", "config/server.toml": "listen = \":8443\"\n",
		}
		if len(got) != len(want) {
			t.Errorf("got %v", got)
		}
		for name, content := range want {
			if got[name] != content {
				t.Errorf("%s = %q, want %q", name, got[name], content)
			}
		}
	})

	t.Run("carries a database bigger than one chunk", func(t *testing.T) {
		contents := dataDir(t)
		big := strings.Repeat("0123456789", 30_000)
		if err := os.WriteFile(contents.Database, []byte(big), 0o600); err != nil {
			t.Fatal(err)
		}
		contents.Config = filepath.Join(t.TempDir(), "missing.toml")
		got, err := read(written(t, contents), passphrase)
		if err != nil || got["sentinel.db"] != big {
			t.Errorf("got %d bytes, %v", len(got["sentinel.db"]), err)
		}
		if _, ok := got["config/server.toml"]; ok {
			t.Error("a missing config file was backed up")
		}
	})

	t.Run("doesn't leave the contents readable in the file", func(t *testing.T) {
		contents := dataDir(t)
		if file := written(t, contents); bytes.Contains(file, []byte("0123456789abcdef")) || bytes.Contains(file, []byte("certificate")) {
			t.Error("the backup holds the key in the clear")
		}
	})
}

func TestRefusals(t *testing.T) {
	contents := dataDir(t)
	contents.Database = filepath.Join(t.TempDir(), "big.db")
	random := make([]byte, 200_000) // doesn't compress, so it takes several chunks
	_, _ = rand.Read(random)
	if err := os.WriteFile(contents.Database, random, 0o600); err != nil {
		t.Fatal(err)
	}
	file := written(t, contents)
	header := len("SENTINEL BACKUP\n") + 1 + 4 + 4 + 1 + 16 + 7
	cases := []struct {
		name   string
		change func([]byte) []byte
		phrase string
		want   error
	}{
		{"a wrong passphrase", func(f []byte) []byte { return f }, "incorrect horse battery staple", backup.ErrWrongPassphrase},
		{"a file that isn't a backup", func([]byte) []byte { return []byte("PK\x03\x04 a zip file, padded out to be long enough") }, passphrase, backup.ErrNotABackup},
		{"an empty file", func([]byte) []byte { return nil }, passphrase, backup.ErrNotABackup},
		{"a newer version", func(f []byte) []byte { f[16] = 2; return f }, passphrase, backup.ErrNotABackup},
		{"a key derivation that would take forever", func(f []byte) []byte { f[17] = 0xff; return f }, passphrase, backup.ErrNotABackup},
		{"a changed salt", func(f []byte) []byte { f[30] ^= 1; return f }, passphrase, backup.ErrWrongPassphrase},
		{"a changed byte in the middle", func(f []byte) []byte { f[len(f)/2] ^= 1; return f }, passphrase, backup.ErrWrongPassphrase},
		{"a file cut short at a chunk's end", func(f []byte) []byte { return f[:header+4+64<<10+16] }, passphrase, backup.ErrWrongPassphrase},
		{"a file cut short mid-chunk", func(f []byte) []byte { return f[:len(f)-10] }, passphrase, backup.ErrWrongPassphrase},
		{"something after the last chunk", func(f []byte) []byte { return append(f, 0, 0, 0, 0) }, passphrase, backup.ErrWrongPassphrase},
		{"a chunk with an impossible length", func(f []byte) []byte { f[header] = 0xff; return f }, passphrase, backup.ErrWrongPassphrase},
	}
	for _, tc := range cases {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			changed := tc.change(bytes.Clone(file))
			if _, err := read(changed, tc.phrase); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("refuses to write with a short passphrase", func(t *testing.T) {
		if err := backup.Write(io.Discard, []byte("short"), contents); err == nil {
			t.Error("a short passphrase was accepted")
		}
	})

	t.Run("reports a database it can't read", func(t *testing.T) {
		broken := contents
		broken.Database = filepath.Join(t.TempDir(), "missing.db")
		if err := backup.Write(io.Discard, []byte(passphrase), broken); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("hands on an error from the caller", func(t *testing.T) {
		stop := errors.New("disk full")
		err := backup.Read(bytes.NewReader(file), []byte(passphrase), func(string, io.Reader) error { return stop })
		if !errors.Is(err, stop) {
			t.Errorf("got %v", err)
		}
	})
}

func TestUnsafeEntries(t *testing.T) {
	cases := []struct {
		name  string
		entry tar.Header
	}{
		{"a path out of the data directory", tar.Header{Name: "../etc/passwd", Typeflag: tar.TypeReg}},
		{"an absolute path", tar.Header{Name: "/etc/passwd", Typeflag: tar.TypeReg}},
		{"a path that isn't clean", tar.Header{Name: "tls/../secret.key", Typeflag: tar.TypeReg}},
		{"a Windows path", tar.Header{Name: `tls\x`, Typeflag: tar.TypeReg}},
		{"a symbolic link", tar.Header{Name: "secret.key", Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"}},
	}
	for _, tc := range cases {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			file := backup.WriteArchiveForTest(t, []byte(passphrase), tc.entry)
			if _, err := read(file, passphrase); err == nil || !strings.Contains(err.Error(), "unexpected entry") {
				t.Errorf("got %v", err)
			}
		})
	}
}

func FuzzRead(f *testing.F) {
	var out bytes.Buffer
	dir := f.TempDir()
	database := filepath.Join(dir, "db")
	if err := os.WriteFile(database, []byte("data"), 0o600); err != nil {
		f.Fatal(err)
	}
	if err := backup.Write(&out, []byte(passphrase), backup.Contents{DataDir: f.TempDir(), Database: database}); err != nil {
		f.Fatal(err)
	}
	f.Add(out.Bytes())
	f.Add([]byte("SENTINEL BACKUP\n"))
	f.Fuzz(func(t *testing.T, file []byte) {
		_ = backup.Read(bytes.NewReader(file), []byte(passphrase), func(_ string, data io.Reader) error {
			_, err := io.Copy(io.Discard, data)
			return err
		})
	})
}

// failingWriter accepts limit bytes, then fails.
type failingWriter struct{ limit int }

func (f *failingWriter) Write(data []byte) (int, error) {
	if len(data) > f.limit {
		n := f.limit
		f.limit = 0
		return n, errors.New("disk full")
	}
	f.limit -= len(data)
	return len(data), nil
}

func TestWriteFailures(t *testing.T) {
	contents := dataDir(t)
	random := make([]byte, 150_000)
	_, _ = rand.Read(random)
	if err := os.WriteFile(contents.Database, random, 0o600); err != nil {
		t.Fatal(err)
	}
	size := len(written(t, contents))
	for _, limit := range []int{0, 10, 60, 64, size / 2, size - 20, size - 2} {
		t.Run("reports a disk that fills after "+strconv.Itoa(limit)+" bytes", func(t *testing.T) {
			if err := backup.Write(&failingWriter{limit: limit}, []byte(passphrase), contents); err == nil {
				t.Error("expected an error")
			}
		})
	}
	t.Run("reports a data directory it can't read", func(t *testing.T) {
		broken := contents
		broken.DataDir = filepath.Join(t.TempDir(), "missing")
		if err := backup.Write(io.Discard, []byte(passphrase), broken); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("reports a config file it can't read", func(t *testing.T) {
		broken := contents
		broken.Config = t.TempDir() // a folder
		if err := backup.Write(io.Discard, []byte(passphrase), broken); err == nil {
			t.Error("expected an error")
		}
	})
}

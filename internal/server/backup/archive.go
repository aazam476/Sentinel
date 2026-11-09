// SPDX-License-Identifier: Apache-2.0

package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DatabaseName is the database's name in the data directory and the backup.
const DatabaseName = "sentinel.db"

// ConfigName is where the configuration file sits in a backup.
const ConfigName = "config/server.toml"

// maxEntrySize bounds one file in a backup, against a crafted archive
// filling the disk: far more than any real database or key.
const maxEntrySize = 1 << 40

// skipped are data directory files a backup leaves out: the live database
// (a consistent copy is taken instead), its journal files, and the setup
// token, which is only for a server that has never been set up.
func skipped(name string) bool {
	switch name {
	case DatabaseName, DatabaseName + "-wal", DatabaseName + "-shm", DatabaseName + "-journal", "setup-token":
		return true
	}
	return strings.HasSuffix(name, ".tmp")
}

// Contents says what goes into a backup.
type Contents struct {
	// DataDir is the server's data directory: its keys and certificates are
	// backed up.
	DataDir string
	// Database is a consistent copy of the database, made for the backup.
	Database string
	// Config is the configuration file; "" or a missing file leaves it out.
	Config string
}

// Write writes an encrypted backup of contents to out.
func Write(out io.Writer, passphrase []byte, contents Contents) error {
	encrypter, err := newEncrypter(out, passphrase)
	if err != nil {
		return err
	}
	compressed := gzip.NewWriter(encrypter)
	archive := tar.NewWriter(compressed)
	if err := addFile(archive, DatabaseName, contents.Database); err != nil {
		return err
	}
	if err := addDataDir(archive, contents.DataDir); err != nil {
		return err
	}
	if contents.Config != "" {
		if err := addFile(archive, ConfigName, contents.Config); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := errors.Join(archive.Close(), compressed.Close()); err != nil {
		return fmt.Errorf("write the backup: %w", err)
	}
	return encrypter.Close()
}

func addDataDir(archive *tar.Writer, dataDir string) error {
	return filepath.WalkDir(dataDir, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read the data directory: %w", err)
		}
		relative, err := filepath.Rel(dataDir, file)
		if err != nil {
			return fmt.Errorf("read the data directory: %w", err)
		}
		if !entry.Type().IsRegular() || skipped(relative) {
			return nil
		}
		return addFile(archive, filepath.ToSlash(relative), file)
	})
}

func addFile(archive *tar.Writer, name, file string) error {
	opened, err := os.Open(file) //nolint:gosec // G304: a file in the server's own data directory or its config
	if err != nil {
		return fmt.Errorf("back up %s: %w", name, err)
	}
	defer func() { _ = opened.Close() }() // read-only
	info, err := opened.Stat()
	if err != nil {
		return fmt.Errorf("back up %s: %w", name, err)
	}
	if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return fmt.Errorf("back up %s: %w", name, err)
	}
	if _, err := io.Copy(archive, opened); err != nil {
		return fmt.Errorf("back up %s: %w", name, err)
	}
	return nil
}

// Read decrypts a backup and hands each file in it to keep, with its name
// (a clean relative path; the configuration file is "config/server.toml").
// The whole backup is checked as it is read: an error from Read means
// some of it was wrong, so files already handed over must be thrown away.
func Read(in io.Reader, passphrase []byte, keep func(name string, data io.Reader) error) error {
	decrypter, err := newDecrypter(in, passphrase)
	if err != nil {
		return err
	}
	compressed, err := gzip.NewReader(decrypter)
	if err != nil {
		return ErrWrongPassphrase
	}
	archive := tar.NewReader(compressed)
	for {
		entry, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return wrapped(err)
		}
		if entry.Typeflag != tar.TypeReg || !safeName(entry.Name) || entry.Size < 0 || entry.Size > maxEntrySize {
			return fmt.Errorf("the backup holds an unexpected entry %q", entry.Name)
		}
		if err := keep(entry.Name, archive); err != nil {
			return wrapped(err)
		}
	}
	// Reading to the end checks the last chunk.
	if _, err := io.Copy(io.Discard, decrypter); err != nil {
		return err
	}
	return nil
}

// wrapped reports a decryption failure as such, and other errors as they are.
func wrapped(err error) error {
	if errors.Is(err, ErrWrongPassphrase) {
		return ErrWrongPassphrase
	}
	return err
}

// safeName accepts a relative path inside the data directory: no "..", no
// absolute path, nothing that cleans to something else.
func safeName(name string) bool {
	return name != "" && path.Clean(name) == name && !path.IsAbs(name) && name != "." &&
		!strings.HasPrefix(name, "../") && name != ".." && !strings.Contains(name, "\\") && !strings.Contains(name, ":")
}

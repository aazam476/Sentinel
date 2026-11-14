// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package files_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/files"
	"github.com/aazam476/sentinel/internal/protocol"
)

// shadow is what tree puts in secret/shadow, outside the allowed folder.
const shadow = "root:x:0:0"

// partialOf is where an upload to path keeps its data until it is
// complete: a hidden file named after the destination and the upload's ID.
func partialOf(path, uploadID string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+uploadID+".sentinel-upload")
}

// uploadIDOf learns the ID an upload will have the way anyone with the
// file manager can: by beginning it and cancelling it.
func uploadIDOf(t *testing.T, m *files.Manager, path string, size uint64) string {
	t.Helper()
	started, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: size})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CancelUpload(started.UploadID); err != nil {
		t.Fatal(err)
	}
	return started.UploadID
}

// expectUntouched checks nothing outside the allowed folder changed: the
// secret file's content and mode, and no new file beside it.
func expectUntouched(t *testing.T, secret string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(secret, "shadow"))
	if err != nil || string(content) != shadow {
		t.Errorf("the file outside now holds %q (%v)", content, err)
	}
	if info, err := os.Stat(filepath.Join(secret, "shadow")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the file outside now has mode %v (%v)", info.Mode().Perm(), err)
	}
	if entries, err := os.ReadDir(secret); err != nil || len(entries) != 1 {
		t.Errorf("the folder outside now holds %d entries (%v)", len(entries), err)
	}
}

func TestSEC25_AnUploadNeverWritesThroughALink(t *testing.T) {
	cases := []struct {
		name string
		// plant puts something where the upload's data will go.
		plant func(m *files.Manager, partial, allowed, secret string) error
	}{
		{"a link to a file outside", func(_ *files.Manager, partial, _, secret string) error {
			return os.Symlink(filepath.Join(secret, "shadow"), partial)
		}},
		{"a link from the folder, moved there with the file manager", func(m *files.Manager, partial, allowed, secret string) error {
			link := filepath.Join(allowed, "current")
			if err := os.Symlink(filepath.Join(secret, "shadow"), link); err != nil {
				return err
			}
			return m.Rename(protocol.FileRenamePayload{From: link, To: partial})
		}},
		{"a link to a file that isn't there yet", func(_ *files.Manager, partial, _, secret string) error {
			return os.Symlink(filepath.Join(secret, "planted"), partial)
		}},
		{"a second name for a file outside", func(_ *files.Manager, partial, _, secret string) error {
			return os.Link(filepath.Join(secret, "shadow"), partial)
		}},
		{"a folder", func(_ *files.Manager, partial, _, _ string) error { return os.Mkdir(partial, 0o750) }},
	}
	for _, tc := range cases {
		t.Run("SEC-25 refuses to begin when its data would go to "+tc.name, func(t *testing.T) {
			_, allowed, secret := tree(t)
			m := files.New([]string{allowed})
			path := filepath.Join(allowed, "report.txt")
			size := uint64(len(shadow) + len("evil"))
			partial := partialOf(path, uploadIDOf(t, m, path, size))
			if err := tc.plant(m, partial, allowed, secret); err != nil {
				t.Fatal(err)
			}
			started, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: size})
			if !errors.Is(err, files.ErrPartialInTheWay) {
				t.Errorf("got %+v, %v, want ErrPartialInTheWay", started, err)
			}
			expectUntouched(t, secret)
		})
	}

	// The link is swapped in once the upload has begun.
	steps := []struct {
		name string
		size uint64
		do   func(m *files.Manager, uploadID string) error
	}{
		{"write a chunk", uint64(len(shadow) + len("evil")), func(m *files.Manager, uploadID string) error {
			_, err := m.WriteUpload(protocol.FileUploadWritePayload{UploadID: uploadID, Offset: uint64(len(shadow)), Data: []byte("evil")})
			return err
		}},
		{"finish", uint64(len(shadow)), func(m *files.Manager, uploadID string) error {
			_, err := m.FinishUpload(uploadID)
			return err
		}},
	}
	for _, tc := range steps {
		t.Run("SEC-25 refuses to "+tc.name+" when a link has taken the place of its data", func(t *testing.T) {
			_, allowed, secret := tree(t)
			m := files.New([]string{allowed})
			path := filepath.Join(allowed, "report.txt")
			started, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: tc.size})
			if err != nil {
				t.Fatal(err)
			}
			partial := partialOf(path, started.UploadID)
			if err := os.Remove(partial); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(secret, "shadow"), partial); err != nil {
				t.Fatal(err)
			}
			if err := tc.do(m, started.UploadID); !errors.Is(err, files.ErrPartialInTheWay) {
				t.Errorf("got %v, want ErrPartialInTheWay", err)
			}
			expectUntouched(t, secret)
			if _, err := os.Lstat(path); err == nil {
				t.Error("the link was moved into place as the uploaded file")
			}
		})
	}
}

func TestUploadedFileModes(t *testing.T) {
	upload := func(t *testing.T, m *files.Manager, path string, overwrite bool) os.FileMode {
		t.Helper()
		started, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: 3, Overwrite: overwrite})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.WriteUpload(protocol.FileUploadWritePayload{UploadID: started.UploadID, Data: []byte("abc")}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.FinishUpload(started.UploadID); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}

	t.Run("a new file is an ordinary one, readable by everyone", func(t *testing.T) {
		_, allowed, _ := tree(t)
		if mode := upload(t, files.New(nil), filepath.Join(allowed, "new.txt"), false); mode != 0o644 {
			t.Errorf("mode %v, want 0644", mode)
		}
	})

	t.Run("a file it replaces keeps its mode, so a private file stays private", func(t *testing.T) {
		_, allowed, _ := tree(t)
		// tree writes notes.txt with mode 0600.
		if mode := upload(t, files.New(nil), filepath.Join(allowed, "notes.txt"), true); mode != 0o600 {
			t.Errorf("mode %v, want the 0600 it had", mode)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package files_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/files"
	"github.com/aazam476/sentinel/internal/protocol"
)

// tree makes a folder for a test: allowed/ with a file, a subfolder and a
// link out to secret/, which sits beside it, outside what is allowed.
func tree(t *testing.T) (root, allowed, secret string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	allowed, secret = filepath.Join(root, "allowed"), filepath.Join(root, "secret")
	for _, folder := range []string{allowed, filepath.Join(allowed, "Photos"), secret} {
		if err := os.Mkdir(folder, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(allowed, "notes.txt"), "hello\n")
	write(t, filepath.Join(secret, "shadow"), "root:x:0:0")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(secret, filepath.Join(allowed, "escape")); err != nil {
			t.Fatal(err)
		}
	}
	return root, allowed, secret
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func names(entries []protocol.FileEntry) []string {
	result := []string{}
	for _, entry := range entries {
		result = append(result, entry.Name+":"+entry.Type)
	}
	return result
}

func TestList(t *testing.T) {
	t.Run("lists a folder, folders first, with each entry's kind and size", func(t *testing.T) {
		_, allowed, _ := tree(t)
		listing, err := files.New(nil).List(allowed)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Photos:dir", "escape:symlink", "notes.txt:file"}
		if runtime.GOOS == "windows" {
			want = []string{"Photos:dir", "notes.txt:file"}
		}
		if got := names(listing.Entries); !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if listing.Parent != filepath.Dir(allowed) || listing.Entries[len(listing.Entries)-1].SizeBytes != 6 {
			t.Errorf("listing %+v", listing)
		}
	})

	t.Run("starts at the file system's roots, or at the allowed folders", func(t *testing.T) {
		listing, err := files.New(nil).List("")
		if err != nil || len(listing.Entries) == 0 || listing.Path != "" {
			t.Fatalf("got %+v, %v", listing, err)
		}
		_, allowed, _ := tree(t)
		listing, err = files.New([]string{allowed}).List("")
		if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Path != allowed {
			t.Errorf("got %+v, %v", listing, err)
		}
	})

	t.Run("has no parent at an allowed folder, so browsing can't climb out", func(t *testing.T) {
		_, allowed, _ := tree(t)
		listing, err := files.New([]string{allowed}).List(allowed)
		if err != nil || listing.Parent != "" {
			t.Errorf("got %+v, %v", listing, err)
		}
	})
}

func TestSEC25_PathRestrictions(t *testing.T) {
	cases := []struct {
		name string
		do   func(m *files.Manager, root, allowed, secret string) error
	}{
		{"lists a folder outside", func(m *files.Manager, _, _, secret string) error { _, err := m.List(secret); return err }},
		{"reads a file outside", func(m *files.Manager, _, _, secret string) error {
			_, err := m.Read(protocol.FileReadPayload{Path: filepath.Join(secret, "shadow"), Length: 10})
			return err
		}},
		{"climbs out with ..", func(m *files.Manager, _, allowed, _ string) error {
			_, err := m.ReadText(allowed + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Join("secret", "shadow"))
			return err
		}},
		{"lists a sibling whose name starts like an allowed folder's", func(m *files.Manager, root, _, _ string) error {
			if err := os.Mkdir(filepath.Join(root, "allowed2"), 0o750); err != nil {
				return err
			}
			_, err := m.List(filepath.Join(root, "allowed2"))
			return err
		}},
		{"creates a folder outside", func(m *files.Manager, root, _, _ string) error { return m.Mkdir(filepath.Join(root, "new")) }},
		{"moves a file out", func(m *files.Manager, root, allowed, _ string) error {
			return m.Rename(protocol.FileRenamePayload{From: filepath.Join(allowed, "notes.txt"), To: filepath.Join(root, "notes.txt")})
		}},
		{"uploads outside", func(m *files.Manager, _, _, secret string) error {
			_, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: filepath.Join(secret, "x"), SizeBytes: 1})
			return err
		}},
		{"writes a text file outside", func(m *files.Manager, _, _, secret string) error {
			_, err := m.WriteText(protocol.FileTextPayload{Path: filepath.Join(secret, "x"), Content: "x"})
			return err
		}},
	}
	if runtime.GOOS != "windows" {
		cases = append(cases,
			struct {
				name string
				do   func(m *files.Manager, root, allowed, secret string) error
			}{"follows a link out of the allowed folder", func(m *files.Manager, _, allowed, _ string) error {
				_, err := m.Read(protocol.FileReadPayload{Path: filepath.Join(allowed, "escape", "shadow"), Length: 10})
				return err
			}},
			struct {
				name string
				do   func(m *files.Manager, root, allowed, secret string) error
			}{"writes through a link out of the allowed folder", func(m *files.Manager, _, allowed, _ string) error {
				_, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: filepath.Join(allowed, "escape", "planted"), SizeBytes: 1})
				return err
			}},
		)
	}
	for _, tc := range cases {
		t.Run("SEC-25 refuses when it "+tc.name, func(t *testing.T) {
			root, allowed, secret := tree(t)
			if err := tc.do(files.New([]string{allowed}), root, allowed, secret); !errors.Is(err, files.ErrNotAllowed) {
				t.Errorf("got %v, want ErrNotAllowed", err)
			}
		})
	}

	t.Run("SEC-25 still works inside the allowed folder, and deletes a link without following it", func(t *testing.T) {
		_, allowed, secret := tree(t)
		m := files.New([]string{allowed})
		if _, err := m.ReadText(filepath.Join(allowed, "notes.txt")); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "windows" {
			return
		}
		if err := m.Delete(protocol.FileDeletePayload{Path: filepath.Join(allowed, "escape"), Recursive: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(secret, "shadow")); err != nil {
			t.Errorf("deleting the link touched its target: %v", err)
		}
	})
}

func TestPaths(t *testing.T) {
	for _, path := range []string{"", "relative/path", "notes.txt", "/tmp/\x00evil"} {
		t.Run("refuses the path "+strings.ReplaceAll(path, "\x00", `\0`), func(t *testing.T) {
			if _, err := files.New(nil).Stat(path); !errors.Is(err, files.ErrInvalidPath) {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestReadAndChange(t *testing.T) {
	t.Run("reads a file in chunks, with its size", func(t *testing.T) {
		_, allowed, _ := tree(t)
		m := files.New(nil)
		chunk, err := m.Read(protocol.FileReadPayload{Path: filepath.Join(allowed, "notes.txt"), Offset: 2, Length: 2})
		if err != nil || string(chunk.Data) != "ll" || chunk.SizeBytes != 6 {
			t.Errorf("got %+v, %v", chunk, err)
		}
		chunk, err = m.Read(protocol.FileReadPayload{Path: filepath.Join(allowed, "notes.txt"), Offset: 99, Length: 2})
		if err != nil || len(chunk.Data) != 0 {
			t.Errorf("past the end: %+v, %v", chunk, err)
		}
		if _, err := m.Read(protocol.FileReadPayload{Path: allowed, Length: 2}); !errors.Is(err, files.ErrIsFolder) {
			t.Errorf("a folder: %v", err)
		}
	})

	t.Run("creates, renames and deletes, refusing to replace or to delete a starting folder", func(t *testing.T) {
		_, allowed, _ := tree(t)
		m := files.New([]string{allowed})
		created := filepath.Join(allowed, "new")
		if err := m.Mkdir(created); err != nil {
			t.Fatal(err)
		}
		if err := m.Mkdir(created); !errors.Is(err, files.ErrExists) {
			t.Errorf("again: %v", err)
		}
		renamed := filepath.Join(allowed, "Photos", "moved")
		if err := m.Rename(protocol.FileRenamePayload{From: created, To: renamed}); err != nil {
			t.Fatal(err)
		}
		if err := m.Rename(protocol.FileRenamePayload{From: filepath.Join(allowed, "notes.txt"), To: renamed}); !errors.Is(err, files.ErrExists) {
			t.Errorf("onto an existing entry: %v", err)
		}
		if err := m.Delete(protocol.FileDeletePayload{Path: filepath.Join(allowed, "Photos")}); !errors.Is(err, files.ErrNotEmpty) {
			t.Errorf("a full folder: %v", err)
		}
		if err := m.Delete(protocol.FileDeletePayload{Path: filepath.Join(allowed, "Photos"), Recursive: true}); err != nil {
			t.Fatal(err)
		}
		if err := m.Delete(protocol.FileDeletePayload{Path: allowed, Recursive: true}); !errors.Is(err, files.ErrProtected) {
			t.Errorf("the allowed folder: %v", err)
		}
		if err := m.Rename(protocol.FileRenamePayload{From: allowed, To: allowed + "-gone"}); !errors.Is(err, files.ErrNotAllowed) && !errors.Is(err, files.ErrProtected) {
			t.Errorf("moving the allowed folder: %v", err)
		}
		if err := m.Delete(protocol.FileDeletePayload{Path: filepath.Join(allowed, "nothing")}); !errors.Is(err, files.ErrNotFound) {
			t.Errorf("nothing there: %v", err)
		}
	})
}

func TestText(t *testing.T) {
	t.Run("edits a text file, keeping its mode, and refuses if it changed since it was read", func(t *testing.T) {
		_, allowed, _ := tree(t)
		m := files.New(nil)
		path := filepath.Join(allowed, "notes.txt")
		text, err := m.ReadText(path)
		if err != nil || text.Content != "hello\n" {
			t.Fatalf("got %+v, %v", text, err)
		}
		text.Content = "hello again\n"
		if _, err := m.WriteText(text); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(path); string(data) != "hello again\n" {
			t.Errorf("saved %q", data)
		}
		if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, want it kept at 0600", info.Mode().Perm())
		}
		stale := text
		stale.Modified = time.Now().Add(-time.Hour).UnixMilli()
		if _, err := m.WriteText(stale); !errors.Is(err, files.ErrChanged) {
			t.Errorf("a stale save: %v", err)
		}
	})

	t.Run("creates a text file, but not over one that exists", func(t *testing.T) {
		_, allowed, _ := tree(t)
		m := files.New(nil)
		if _, err := m.WriteText(protocol.FileTextPayload{Path: filepath.Join(allowed, "new.conf"), Content: "a = 1\n"}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.WriteText(protocol.FileTextPayload{Path: filepath.Join(allowed, "notes.txt"), Content: "x"}); !errors.Is(err, files.ErrChanged) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("won't open a binary or large file as text", func(t *testing.T) {
		_, allowed, _ := tree(t)
		write(t, filepath.Join(allowed, "image.bin"), "\x89PNG\x00\x01")
		write(t, filepath.Join(allowed, "big.log"), strings.Repeat("x", protocol.MaxTextFile+1))
		for _, name := range []string{"image.bin", "big.log"} {
			if _, err := files.New(nil).ReadText(filepath.Join(allowed, name)); !errors.Is(err, files.ErrNotText) {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}

func TestUpload(t *testing.T) {
	t.Run("uploads in chunks, resumes after a drop, and only then appears", func(t *testing.T) {
		_, allowed, _ := tree(t)
		path := filepath.Join(allowed, "backup.tar")
		data := bytes.Repeat([]byte("0123456789"), 100)
		m := files.New(nil)
		started, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: uint64(len(data))})
		if err != nil || started.ReceivedBytes != 0 {
			t.Fatalf("got %+v, %v", started, err)
		}
		if _, err := m.WriteUpload(protocol.FileUploadWritePayload{UploadID: started.UploadID, Data: data[:400]}); err != nil {
			t.Fatal(err)
		}
		// The agent restarts: a new Manager knows nothing of the upload,
		// until it begins again and finds where it stopped.
		m = files.New(nil)
		if _, err := m.WriteUpload(protocol.FileUploadWritePayload{UploadID: started.UploadID, Offset: 400, Data: data[400:]}); !errors.Is(err, files.ErrNoUpload) {
			t.Errorf("an unknown upload: %v", err)
		}
		resumed, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: uint64(len(data))})
		if err != nil || resumed.UploadID != started.UploadID || resumed.ReceivedBytes != 400 {
			t.Fatalf("got %+v, %v", resumed, err)
		}
		if _, err := os.Stat(path); err == nil {
			t.Error("the file appeared before the upload finished")
		}
		if _, err := m.WriteUpload(protocol.FileUploadWritePayload{UploadID: started.UploadID, Offset: 0, Data: data[:400]}); !errors.Is(err, files.ErrWrongOffset) {
			t.Errorf("a repeated chunk: %v", err)
		}
		if _, err := m.FinishUpload(started.UploadID); !errors.Is(err, files.ErrIncomplete) {
			t.Errorf("finished early: %v", err)
		}
		if _, err := m.WriteUpload(protocol.FileUploadWritePayload{UploadID: started.UploadID, Offset: 400, Data: data[400:]}); err != nil {
			t.Fatal(err)
		}
		entry, err := m.FinishUpload(started.UploadID)
		if err != nil || entry.SizeBytes != uint64(len(data)) {
			t.Fatalf("got %+v, %v", entry, err)
		}
		if saved, _ := os.ReadFile(path); !bytes.Equal(saved, data) {
			t.Error("the file isn't what was uploaded")
		}
		if leftovers, _ := filepath.Glob(filepath.Join(allowed, ".*.sentinel-upload")); len(leftovers) != 0 {
			t.Errorf("left %v behind", leftovers)
		}
	})

	t.Run("refuses to replace a file unless asked, and more data than the size", func(t *testing.T) {
		_, allowed, _ := tree(t)
		m := files.New(nil)
		path := filepath.Join(allowed, "notes.txt")
		if _, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: 3}); !errors.Is(err, files.ErrExists) {
			t.Errorf("got %v", err)
		}
		started, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: 3, Overwrite: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.WriteUpload(protocol.FileUploadWritePayload{UploadID: started.UploadID, Data: []byte("abcd")}); !errors.Is(err, files.ErrTooLong) {
			t.Errorf("got %v", err)
		}
		if _, err := m.WriteUpload(protocol.FileUploadWritePayload{UploadID: started.UploadID, Data: []byte("abc")}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.FinishUpload(started.UploadID); err != nil {
			t.Fatal(err)
		}
		if saved, _ := os.ReadFile(path); string(saved) != "abc" {
			t.Errorf("saved %q", saved)
		}
	})

	t.Run("cancels an upload, deleting what arrived", func(t *testing.T) {
		_, allowed, _ := tree(t)
		m := files.New(nil)
		started, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: filepath.Join(allowed, "big.iso"), SizeBytes: 10})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.CancelUpload(started.UploadID); err != nil {
			t.Fatal(err)
		}
		if leftovers, _ := filepath.Glob(filepath.Join(allowed, ".*.sentinel-upload")); len(leftovers) != 0 {
			t.Errorf("left %v behind", leftovers)
		}
		if err := m.CancelUpload(started.UploadID); !errors.Is(err, files.ErrNoUpload) {
			t.Errorf("again: %v", err)
		}
	})
}

func TestFailures(t *testing.T) {
	cases := []struct {
		name string
		do   func(m *files.Manager, allowed string) error
		want error
	}{
		{"lists a folder that isn't there", func(m *files.Manager, allowed string) error {
			_, err := m.List(filepath.Join(allowed, "nothing"))
			return err
		}, files.ErrNotFound},
		{"lists a file as a folder", func(m *files.Manager, allowed string) error {
			_, err := m.List(filepath.Join(allowed, "notes.txt"))
			return err
		}, files.ErrNotFolder},
		{"reads a file that isn't there", func(m *files.Manager, allowed string) error {
			_, err := m.Read(protocol.FileReadPayload{Path: filepath.Join(allowed, "nothing"), Length: 1})
			return err
		}, files.ErrNotFound},
		{"opens a folder as text", func(m *files.Manager, allowed string) error {
			_, err := m.ReadText(allowed)
			return err
		}, files.ErrIsFolder},
		{"saves text over a folder", func(m *files.Manager, allowed string) error {
			_, err := m.WriteText(protocol.FileTextPayload{Path: filepath.Join(allowed, "Photos"), Content: "x"})
			return err
		}, files.ErrIsFolder},
		{"saves a file deleted since it was read", func(m *files.Manager, allowed string) error {
			_, err := m.WriteText(protocol.FileTextPayload{Path: filepath.Join(allowed, "gone.txt"), Content: "x", Modified: 1})
			return err
		}, files.ErrChanged},
		{"saves more than a text file may hold", func(m *files.Manager, allowed string) error {
			_, err := m.WriteText(protocol.FileTextPayload{Path: filepath.Join(allowed, "big.txt"), Content: strings.Repeat("x", protocol.MaxTextFile+1)})
			return err
		}, files.ErrNotText},
		{"creates a folder inside one that isn't there", func(m *files.Manager, allowed string) error {
			return m.Mkdir(filepath.Join(allowed, "nothing", "new"))
		}, files.ErrNotFound},
		{"creates something named ..", func(m *files.Manager, allowed string) error {
			return m.Mkdir(allowed + string(filepath.Separator) + "Photos" + string(filepath.Separator) + "..")
		}, files.ErrExists},
		{"renames something that isn't there", func(m *files.Manager, allowed string) error {
			return m.Rename(protocol.FileRenamePayload{From: filepath.Join(allowed, "nothing"), To: filepath.Join(allowed, "x")})
		}, files.ErrNotFound},
		{"finishes an upload that isn't there", func(m *files.Manager, _ string) error {
			_, err := m.FinishUpload("abc")
			return err
		}, files.ErrNoUpload},
		{"finishes an upload when a file appeared at its destination", func(m *files.Manager, allowed string) error {
			path := filepath.Join(allowed, "late.txt")
			started, err := m.BeginUpload(protocol.FileUploadBeginPayload{Path: path, SizeBytes: 0})
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
				return err
			}
			_, err = m.FinishUpload(started.UploadID)
			return err
		}, files.ErrExists},
	}
	for _, tc := range cases {
		t.Run("refuses when it "+tc.name, func(t *testing.T) {
			_, allowed, _ := tree(t)
			if err := tc.do(files.New(nil), allowed); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("describes a file and a folder", func(t *testing.T) {
		_, allowed, _ := tree(t)
		m := files.New(nil)
		file, err := m.Stat(filepath.Join(allowed, "notes.txt"))
		if err != nil || file.Type != "file" || file.SizeBytes != 6 {
			t.Errorf("got %+v, %v", file, err)
		}
		folder, err := m.Stat(allowed)
		if err != nil || folder.Type != "dir" || folder.SizeBytes != 0 {
			t.Errorf("got %+v, %v", folder, err)
		}
	})
}

func TestHugeFolder(t *testing.T) {
	t.Run("sends at most 5,000 entries of a huge folder, saying it left some out", func(t *testing.T) {
		folder := t.TempDir()
		for i := range 5_001 {
			write(t, filepath.Join(folder, "f"+strconv.Itoa(i)), "")
		}
		listing, err := files.New(nil).List(folder)
		if err != nil || len(listing.Entries) != 5_000 || !listing.Truncated {
			t.Errorf("got %d entries, truncated %v, %v", len(listing.Entries), listing.Truncated, err)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/aazam476/sentinel/internal/protocol"
)

// maxEntries is the most entries one listing sends; a folder with more is
// marked truncated rather than sent in a message too big to deliver.
const maxEntries = 5000

// Manager does what the file manager asks, within the local policy.
type Manager struct {
	guard   guard
	mutex   sync.Mutex
	uploads map[string]upload
}

// New returns a Manager limited to allowed, the local policy's folders
// (SEC-25); none allows the whole file system. The folders are resolved
// through symbolic links, as every path checked against them is.
func New(allowed []string) *Manager {
	resolved := make([]string, 0, len(allowed))
	for _, folder := range allowed {
		if real, err := filepath.EvalSymlinks(folder); err == nil {
			folder = real
		}
		resolved = append(resolved, folder)
	}
	return &Manager{guard: guard{allowed: resolved}, uploads: map[string]upload{}}
}

// List lists a folder, folders first, then by name. An empty path lists
// the starting points.
func (m *Manager) List(path string) (protocol.FileListResultPayload, error) {
	if path == "" {
		return m.starts(), nil
	}
	folder, err := m.guard.existing(path)
	if err != nil {
		return protocol.FileListResultPayload{}, err
	}
	// Windows reports listing a file as "path not found", so the kind is
	// checked here rather than read from the error.
	if info, err := os.Stat(folder); err != nil {
		return protocol.FileListResultPayload{}, describe(err, folder)
	} else if !info.IsDir() {
		return protocol.FileListResultPayload{}, ErrNotFolder
	}
	dirEntries, err := os.ReadDir(folder)
	if err != nil {
		return protocol.FileListResultPayload{}, describe(err, folder)
	}
	result := protocol.FileListResultPayload{Path: folder, Entries: []protocol.FileEntry{}}
	if !m.guard.isStart(folder) {
		result.Parent = filepath.Dir(folder)
	}
	for _, dirEntry := range dirEntries {
		if len(result.Entries) == maxEntries {
			result.Truncated = true
			break
		}
		if entry, err := describeEntry(filepath.Join(folder, dirEntry.Name())); err == nil {
			result.Entries = append(result.Entries, entry)
		}
	}
	sortEntries(result.Entries)
	return result, nil
}

// starts lists where browsing begins: the allowed folders, or the roots.
func (m *Manager) starts() protocol.FileListResultPayload {
	roots := m.guard.allowed
	if len(roots) == 0 {
		roots = fileSystemRoots()
	}
	result := protocol.FileListResultPayload{Entries: []protocol.FileEntry{}}
	for _, root := range roots {
		if entry, err := describeEntry(root); err == nil {
			entry.Name = root
			result.Entries = append(result.Entries, entry)
		}
	}
	return result
}

// fileSystemRoots is / on Unix, and each drive that exists on Windows.
func fileSystemRoots() []string {
	if runtime.GOOS != "windows" {
		return []string{"/"}
	}
	roots := []string{}
	for letter := 'A'; letter <= 'Z'; letter++ {
		root := string(letter) + `:\`
		if _, err := os.Stat(root); err == nil {
			roots = append(roots, root)
		}
	}
	return roots
}

func sortEntries(entries []protocol.FileEntry) {
	slices.SortFunc(entries, func(a, b protocol.FileEntry) int {
		if (a.Type == "dir") != (b.Type == "dir") {
			if a.Type == "dir" {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

// describeEntry describes a path without following it if it is a link.
func describeEntry(path string) (protocol.FileEntry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return protocol.FileEntry{}, describe(err, path)
	}
	entry := protocol.FileEntry{
		Name: info.Name(), Path: path, Type: entryType(info.Mode()), Mode: info.Mode().String(),
		Modified: info.ModTime().UnixMilli(),
	}
	if entry.Type == "file" {
		entry.SizeBytes = uint64(info.Size()) //nolint:gosec // G115: a file's size is never negative
	}
	if entry.Type == "symlink" {
		entry.Target, _ = os.Readlink(path)
	}
	return entry, nil
}

func entryType(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "dir"
	case mode.IsRegular():
		return "file"
	case mode&fs.ModeSymlink != 0:
		return "symlink"
	}
	return "other"
}

// Stat describes one file or folder, following links.
func (m *Manager) Stat(path string) (protocol.FileEntry, error) {
	cleaned, err := m.guard.existing(path)
	if err != nil {
		return protocol.FileEntry{}, err
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return protocol.FileEntry{}, describe(err, cleaned)
	}
	entry := protocol.FileEntry{
		Name: info.Name(), Path: cleaned, Type: entryType(info.Mode()), Mode: info.Mode().String(),
		Modified: info.ModTime().UnixMilli(),
	}
	if entry.Type == "file" {
		entry.SizeBytes = uint64(info.Size()) //nolint:gosec // G115: a file's size is never negative
	}
	return entry, nil
}

// Read reads up to length bytes of a file from offset.
func (m *Manager) Read(request protocol.FileReadPayload) (protocol.FileChunkPayload, error) {
	cleaned, err := m.guard.existing(request.Path)
	if err != nil {
		return protocol.FileChunkPayload{}, err
	}
	file, err := os.Open(cleaned) //nolint:gosec // G304: the path was checked against the local policy
	if err != nil {
		return protocol.FileChunkPayload{}, describe(err, cleaned)
	}
	defer func() { _ = file.Close() }() // read-only; nothing to lose
	info, err := file.Stat()
	if err != nil {
		return protocol.FileChunkPayload{}, describe(err, cleaned)
	}
	if info.IsDir() {
		return protocol.FileChunkPayload{}, ErrIsFolder
	}
	length := min(int64(request.Length), protocol.MaxFileChunk)
	data := make([]byte, length)
	read, err := file.ReadAt(data, int64(min(request.Offset, uint64(info.Size())))) //nolint:gosec // G115: bounded by the file's size
	if err != nil && !errors.Is(err, io.EOF) {
		return protocol.FileChunkPayload{}, describe(err, cleaned)
	}
	return protocol.FileChunkPayload{
		Data: data[:read], SizeBytes: uint64(info.Size()), Modified: info.ModTime().UnixMilli(), //nolint:gosec // G115: a file's size is never negative
	}, nil
}

// Mkdir creates a folder.
func (m *Manager) Mkdir(path string) error {
	cleaned, err := m.guard.newEntry(path)
	if err != nil {
		return err
	}
	if err := os.Mkdir(cleaned, 0o755); err != nil { //nolint:gosec // G301: an ordinary folder, as mkdir makes it
		return describe(err, cleaned)
	}
	return nil
}

// Rename moves a file or folder. It never replaces what is already at the
// destination.
func (m *Manager) Rename(request protocol.FileRenamePayload) error {
	from, err := m.guard.entry(request.From)
	if err != nil {
		return err
	}
	to, err := m.guard.newEntry(request.To)
	if err != nil {
		return err
	}
	if m.guard.isStart(from) {
		return ErrProtected
	}
	if _, err := os.Lstat(to); err == nil {
		return ErrExists
	}
	if err := os.Rename(from, to); err != nil {
		return describe(err, from)
	}
	return nil
}

// Delete removes a file, link or empty folder; with recursive, a folder
// and everything in it. Links are removed, never followed.
func (m *Manager) Delete(request protocol.FileDeletePayload) error {
	cleaned, err := m.guard.entry(request.Path)
	if err != nil {
		return err
	}
	if m.guard.isStart(cleaned) {
		return ErrProtected
	}
	if request.Recursive {
		err = os.RemoveAll(cleaned)
	} else {
		err = os.Remove(cleaned)
	}
	if err != nil {
		return describe(err, cleaned)
	}
	return nil
}

// ReadText reads a text file of up to MaxTextFile bytes for editing.
func (m *Manager) ReadText(path string) (protocol.FileTextPayload, error) {
	cleaned, err := m.guard.existing(path)
	if err != nil {
		return protocol.FileTextPayload{}, err
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return protocol.FileTextPayload{}, describe(err, cleaned)
	}
	if info.IsDir() {
		return protocol.FileTextPayload{}, ErrIsFolder
	}
	if info.Size() > protocol.MaxTextFile {
		return protocol.FileTextPayload{}, ErrNotText
	}
	data, err := os.ReadFile(cleaned) //nolint:gosec // G304: the path was checked against the local policy
	if err != nil {
		return protocol.FileTextPayload{}, describe(err, cleaned)
	}
	if !utf8.Valid(data) || slices.Contains(data, 0) {
		return protocol.FileTextPayload{}, ErrNotText
	}
	return protocol.FileTextPayload{Path: cleaned, Content: string(data), Modified: info.ModTime().UnixMilli()}, nil
}

// WriteText saves a text file, refusing if it changed since it was read
// (or exists, when creating one). The new content replaces the old in one
// step, so a reader never sees half of it, and keeps the file's mode.
func (m *Manager) WriteText(request protocol.FileTextPayload) (protocol.FileEntry, error) {
	if len(request.Content) > protocol.MaxTextFile {
		return protocol.FileEntry{}, ErrNotText
	}
	cleaned, err := m.guard.newEntry(request.Path)
	if err != nil {
		return protocol.FileEntry{}, err
	}
	mode := fs.FileMode(0o644)
	info, err := os.Stat(cleaned)
	switch {
	case err == nil && info.IsDir():
		return protocol.FileEntry{}, ErrIsFolder
	case err == nil && info.ModTime().UnixMilli() != request.Modified:
		return protocol.FileEntry{}, ErrChanged
	case err == nil:
		mode = info.Mode().Perm()
	case !errors.Is(err, fs.ErrNotExist):
		return protocol.FileEntry{}, describe(err, cleaned)
	case request.Modified != 0:
		return protocol.FileEntry{}, ErrChanged // it was deleted since it was read
	}
	if err := replace(cleaned, []byte(request.Content), mode); err != nil {
		return protocol.FileEntry{}, err
	}
	return m.Stat(cleaned)
}

// replace writes data to a temporary file beside path, then renames it
// over path.
func replace(path string, data []byte, mode fs.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".sentinel-*")
	if err != nil {
		return describe(err, path)
	}
	defer func() { _ = os.Remove(temporary.Name()) }() // gone already after a successful rename
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return describe(err, path)
	}
	if err := temporary.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		_ = temporary.Close()
		return describe(err, path)
	}
	if err := temporary.Close(); err != nil {
		return describe(err, path)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return describe(err, path)
	}
	return nil
}

// describe turns an operating system error into one of this package's,
// where one fits, or wraps it with the path.
func describe(err error, path string) error {
	// Go counts "folder not empty" as fs.ErrExist on Unix, so it comes first.
	switch {
	case isNotEmpty(err):
		return ErrNotEmpty
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	case isNotFolder(err):
		return ErrNotFolder
	}
	return fmt.Errorf("%s: %w", path, err)
}

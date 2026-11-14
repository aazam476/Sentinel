// SPDX-License-Identifier: Apache-2.0

package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Errors about uploads. They are never reassigned.
var (
	// ErrNoUpload: the upload isn't known; begin it again to resume.
	ErrNoUpload = errors.New("that upload isn't in progress; start it again to resume")
	// ErrWrongOffset: a chunk doesn't continue where the upload is.
	ErrWrongOffset = errors.New("the chunk doesn't continue where the upload is")
	// ErrTooLong: the chunks add up to more than the upload's size.
	ErrTooLong = errors.New("more data than the upload's size")
	// ErrIncomplete: the upload was finished before all of it arrived.
	ErrIncomplete = errors.New("the upload isn't complete")
	// ErrPartialInTheWay: where the upload keeps its data until it is
	// complete, there is something that isn't that data: a link, a folder,
	// a file with a second name.
	ErrPartialInTheWay = errors.New("something other than this upload's unfinished data is in its place; delete it and start the upload again")
)

// upload is a file being uploaded. Its data goes to a hidden partial file
// beside its destination, which is renamed into place when it is complete.
// The upload's ID comes from its destination and size, so after a dropped
// connection, or the agent restarting, beginning it again finds the
// partial file and resumes where it stopped.
//
// That also makes the partial file's name predictable, and the agent
// writes it as root. So it is only ever opened with openPartial, which
// refuses a link left under that name: by someone on the machine who can
// write the folder, or moved there through the file manager to get data
// out of the allowed folders (SEC-25).
type upload struct {
	path      string
	partial   string
	size      uint64
	overwrite bool
}

func uploadID(path string, size uint64) string {
	sum := sha256.Sum256([]byte(path + "\x00" + strconv.FormatUint(size, 10)))
	return hex.EncodeToString(sum[:12])
}

// BeginUpload starts an upload, or resumes the one already under way to
// the same path with the same size.
func (m *Manager) BeginUpload(request protocol.FileUploadBeginPayload) (protocol.FileUploadPayload, error) {
	path, err := m.guard.newEntry(request.Path)
	if err != nil {
		return protocol.FileUploadPayload{}, err
	}
	if info, err := os.Lstat(path); err == nil && (!request.Overwrite || info.IsDir()) {
		return protocol.FileUploadPayload{}, ErrExists
	}
	id := uploadID(path, request.SizeBytes)
	partial := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+id+".sentinel-upload")
	file, err := openPartial(partial, true)
	if err != nil {
		return protocol.FileUploadPayload{}, err
	}
	info, err := file.Stat()
	_ = file.Close() // only opened to create it and read its size
	if err != nil {
		return protocol.FileUploadPayload{}, describe(err, partial)
	}
	received := uint64(info.Size()) //nolint:gosec // G115: a file's size is never negative
	if received > request.SizeBytes {
		return protocol.FileUploadPayload{}, ErrTooLong
	}
	m.mutex.Lock()
	m.uploads[id] = upload{path: path, partial: partial, size: request.SizeBytes, overwrite: request.Overwrite}
	m.mutex.Unlock()
	return protocol.FileUploadPayload{UploadID: id, ReceivedBytes: received}, nil
}

// openPartial opens an upload's partial file for writing, making it first
// if create is set and it isn't there. It only opens a regular file that
// has no other name, and never through a link.
func openPartial(path string, create bool) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|openWithoutFollowing, 0) //nolint:gosec // G304: beside a checked path, and checked below
	if errors.Is(err, fs.ErrNotExist) && create {
		// O_EXCL only makes a new file: if something appeared under the
		// name meanwhile, even a link to nowhere, it fails.
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: as above
		if errors.Is(err, fs.ErrExist) {
			return nil, ErrPartialInTheWay
		}
	}
	if err != nil {
		// A link, a folder or a pipe under the name fails to open in
		// different ways on different systems; what is there says which.
		if info, statErr := os.Lstat(path); statErr == nil && !info.Mode().IsRegular() {
			return nil, ErrPartialInTheWay
		}
		return nil, describe(err, path)
	}
	if err := checkPartial(path, file); err != nil {
		_ = file.Close() // refused; nothing was written
		return nil, err
	}
	return file, nil
}

// checkPartial makes sure the file that was opened is the regular file
// under the name itself, with no other name. Checking the open file, not
// the path again, leaves no moment in which the name can be swapped.
func checkPartial(path string, file *os.File) error {
	opened, err := file.Stat()
	if err != nil {
		return describe(err, path)
	}
	named, err := os.Lstat(path)
	if err != nil {
		return describe(err, path)
	}
	if !named.Mode().IsRegular() || !os.SameFile(named, opened) || hasOtherNames(opened) {
		return ErrPartialInTheWay
	}
	return nil
}

func (m *Manager) upload(id string) (upload, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	found, ok := m.uploads[id]
	if !ok {
		return upload{}, ErrNoUpload
	}
	return found, nil
}

// WriteUpload appends a chunk at offset, which must be where the upload
// is, so a chunk sent twice after a dropped answer isn't written twice.
func (m *Manager) WriteUpload(request protocol.FileUploadWritePayload) (protocol.FileUploadPayload, error) {
	current, err := m.upload(request.UploadID)
	if err != nil {
		return protocol.FileUploadPayload{}, err
	}
	file, err := openPartial(current.partial, false)
	if err != nil {
		return protocol.FileUploadPayload{}, err
	}
	defer func() { _ = file.Close() }() // closed and checked below on the success path
	info, err := file.Stat()
	if err != nil {
		return protocol.FileUploadPayload{}, describe(err, current.partial)
	}
	received := uint64(info.Size()) //nolint:gosec // G115: a file's size is never negative
	if request.Offset != received {
		return protocol.FileUploadPayload{UploadID: request.UploadID, ReceivedBytes: received}, ErrWrongOffset
	}
	if received+uint64(len(request.Data)) > current.size {
		return protocol.FileUploadPayload{}, ErrTooLong
	}
	if _, err := file.WriteAt(request.Data, int64(received)); err != nil { //nolint:gosec // G115: bounded by the upload's size
		return protocol.FileUploadPayload{}, describe(err, current.partial)
	}
	if err := file.Close(); err != nil {
		return protocol.FileUploadPayload{}, describe(err, current.partial)
	}
	return protocol.FileUploadPayload{UploadID: request.UploadID, ReceivedBytes: received + uint64(len(request.Data))}, nil
}

// FinishUpload moves a complete upload into place. A file it replaces
// keeps its mode, as when a text file is saved: a private file doesn't
// become readable by everyone for having been uploaded again.
func (m *Manager) FinishUpload(id string) (protocol.FileEntry, error) {
	current, err := m.upload(id)
	if err != nil {
		return protocol.FileEntry{}, err
	}
	mode := fs.FileMode(0o644) // an ordinary file, as cp makes it
	replaced, err := os.Lstat(current.path)
	switch {
	case err == nil && !current.overwrite:
		return protocol.FileEntry{}, ErrExists
	case err == nil && replaced.Mode().IsRegular():
		mode = replaced.Mode().Perm()
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return protocol.FileEntry{}, describe(err, current.path)
	}
	if err := sealPartial(current, mode); err != nil {
		return protocol.FileEntry{}, err
	}
	if err := os.Rename(current.partial, current.path); err != nil {
		return protocol.FileEntry{}, fmt.Errorf("move the upload into place: %w", describe(err, current.path))
	}
	m.forget(id)
	return m.Stat(current.path)
}

// sealPartial checks a partial file is complete and gives it its final
// mode, through the open file rather than its name, so neither can be
// done to whatever a link under that name points to.
func sealPartial(current upload, mode fs.FileMode) error {
	file, err := openPartial(current.partial, false)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }() // nothing is written here
	info, err := file.Stat()
	if err != nil {
		return describe(err, current.partial)
	}
	if uint64(info.Size()) != current.size { //nolint:gosec // G115: a file's size is never negative
		return ErrIncomplete
	}
	if err := file.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		return describe(err, current.partial)
	}
	return nil
}

// CancelUpload stops an upload and deletes what arrived of it.
func (m *Manager) CancelUpload(id string) error {
	current, err := m.upload(id)
	if err != nil {
		return err
	}
	m.forget(id)
	if err := os.Remove(current.partial); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return describe(err, current.partial)
	}
	return nil
}

func (m *Manager) forget(id string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.uploads, id)
}

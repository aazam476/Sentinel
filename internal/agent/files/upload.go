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
)

// upload is a file being uploaded. Its data goes to a hidden partial file
// beside its destination, which is renamed into place when it is complete.
// The upload's ID comes from its destination and size, so after a dropped
// connection, or the agent restarting, beginning it again finds the
// partial file and resumes where it stopped.
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
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: beside a checked path
	if err != nil {
		return protocol.FileUploadPayload{}, describe(err, partial)
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
	file, err := os.OpenFile(current.partial, os.O_WRONLY, 0o600)
	if err != nil {
		return protocol.FileUploadPayload{}, describe(err, current.partial)
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

// FinishUpload moves a complete upload into place.
func (m *Manager) FinishUpload(id string) (protocol.FileEntry, error) {
	current, err := m.upload(id)
	if err != nil {
		return protocol.FileEntry{}, err
	}
	info, err := os.Stat(current.partial)
	if err != nil {
		return protocol.FileEntry{}, describe(err, current.partial)
	}
	if uint64(info.Size()) != current.size { //nolint:gosec // G115: a file's size is never negative
		return protocol.FileEntry{}, ErrIncomplete
	}
	if _, err := os.Lstat(current.path); err == nil && !current.overwrite {
		return protocol.FileEntry{}, ErrExists
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return protocol.FileEntry{}, describe(err, current.path)
	}
	if err := os.Chmod(current.partial, 0o644); err != nil { //nolint:gosec // G302: an ordinary file, as cp makes it
		return protocol.FileEntry{}, describe(err, current.partial)
	}
	if err := os.Rename(current.partial, current.path); err != nil {
		return protocol.FileEntry{}, fmt.Errorf("move the upload into place: %w", describe(err, current.path))
	}
	m.forget(id)
	return m.Stat(current.path)
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

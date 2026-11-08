// SPDX-License-Identifier: Apache-2.0

package protocol

// File actions (§9.7). File data moves in chunks of at most
// MaxFileChunk bytes, one request each, so a transfer goes as fast as its
// slowest side and can resume from any offset (ADR 0023).
const (
	ActionFileList         = "file.list"
	ActionFileStat         = "file.stat"
	ActionFileRead         = "file.read"
	ActionFileMkdir        = "file.mkdir"
	ActionFileRename       = "file.rename"
	ActionFileDelete       = "file.delete"
	ActionFileReadText     = "file.text.read"
	ActionFileWriteText    = "file.text.write"
	ActionFileUploadBegin  = "file.upload.begin"
	ActionFileUploadWrite  = "file.upload.write"
	ActionFileUploadFinish = "file.upload.finish"
	ActionFileUploadCancel = "file.upload.cancel"
)

// MaxFileChunk is the most file data one request carries. Base64 makes it
// a third bigger, which still fits in MaxMessageSize.
const MaxFileChunk = 512 << 10

// MaxTextFile is the largest file the browser can view and edit (§9.7).
const MaxTextFile = 1 << 20

// FilePathPayload names one file or folder. An empty path on file.list
// lists the starting points: the allowed folders, or the file system's
// roots (/, or each drive on Windows).
type FilePathPayload struct {
	Path string `json:"path"`
}

// FileEntry is one file or folder.
type FileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Type is "file", "dir", "symlink" or "other" (devices, sockets).
	Type string `json:"type"`
	// SizeBytes is a file's size; 0 for folders.
	SizeBytes uint64 `json:"sizeBytes"`
	// Mode is the permissions as ls shows them, e.g. "-rw-r--r--".
	Mode string `json:"mode"`
	// Modified is UTC Unix milliseconds.
	Modified int64 `json:"modified"`
	// Target is where a symbolic link points.
	Target string `json:"target,omitempty"`
}

// FileListResultPayload answers file.list.
type FileListResultPayload struct {
	// Path is the folder listed, cleaned; empty for the starting points.
	Path string `json:"path"`
	// Parent is the folder above, or empty at a starting point.
	Parent  string      `json:"parent"`
	Entries []FileEntry `json:"entries"`
	// Truncated is set when the folder held more entries than were sent.
	Truncated bool `json:"truncated"`
}

// FileReadPayload asks for part of a file.
type FileReadPayload struct {
	Path   string `json:"path"`
	Offset uint64 `json:"offset"`
	// Length is at most MaxFileChunk.
	Length uint32 `json:"length"`
}

// FileChunkPayload is part of a file, and its size and modification time
// when read, so a download can tell if the file changed underneath it.
type FileChunkPayload struct {
	Data      []byte `json:"data"`
	SizeBytes uint64 `json:"sizeBytes"`
	Modified  int64  `json:"modified"`
}

// FileRenamePayload moves or renames a file or folder.
type FileRenamePayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// FileDeletePayload deletes a file, or a folder with Recursive.
type FileDeletePayload struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

// FileTextPayload is a text file's content, read or to be written.
type FileTextPayload struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// Modified, when writing, is the modification time the browser read;
	// the write is refused if the file changed since. 0 creates a file.
	Modified int64 `json:"modified"`
}

// FileUploadBeginPayload starts, or resumes, an upload.
type FileUploadBeginPayload struct {
	Path      string `json:"path"`
	SizeBytes uint64 `json:"sizeBytes"`
	// Overwrite replaces an existing file at Path.
	Overwrite bool `json:"overwrite"`
}

// FileUploadPayload is an upload in progress, with how much the agent
// has stored.
type FileUploadPayload struct {
	UploadID      string `json:"uploadId"`
	ReceivedBytes uint64 `json:"receivedBytes"`
}

// FileUploadWritePayload adds a chunk to an upload, at Offset, which must
// be what the agent has received so far.
type FileUploadWritePayload struct {
	UploadID string `json:"uploadId"`
	Offset   uint64 `json:"offset"`
	Data     []byte `json:"data"`
}

// FileUploadIDPayload names an upload, to finish or cancel it.
type FileUploadIDPayload struct {
	UploadID string `json:"uploadId"`
}

// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/agent/files"
	"github.com/aazam476/sentinel/internal/protocol"
)

// serveFiles answers every file action on conn with a real file manager,
// as the agent would, until the test ends.
func serveFiles(t *testing.T, conn *websocket.Conn, manager *files.Manager) {
	t.Helper()
	conn.SetReadLimit(protocol.MaxMessageSize) // as the real agent does; upload chunks are big
	go func() {
		for {
			message, frame, err := protocol.ReadAny(t.Context(), conn)
			if err != nil {
				return // the test ended
			}
			if frame != nil || message.Type != protocol.TypeRequest {
				continue
			}
			result, failure := answerFile(manager, message)
			response := protocol.Message{Type: protocol.TypeResponse, ID: message.ID}
			if failure != nil {
				response.Error, result = &protocol.Error{Code: codeFor(failure), Message: failure.Error()}, nil
			}
			if err := protocol.WriteMessage(t.Context(), conn, response, result); err != nil {
				return
			}
		}
	}()
}

func answerFile(m *files.Manager, message protocol.Message) (any, error) {
	decode := func(target any) error { return json.Unmarshal(message.Payload, target) }
	var path protocol.FilePathPayload
	var read protocol.FileReadPayload
	var begin protocol.FileUploadBeginPayload
	var write protocol.FileUploadWritePayload
	var id protocol.FileUploadIDPayload
	switch message.Action {
	case protocol.ActionFileList:
		_ = decode(&path)
		return m.List(path.Path)
	case protocol.ActionFileStat:
		_ = decode(&path)
		return m.Stat(path.Path)
	case protocol.ActionFileRead:
		_ = decode(&read)
		return m.Read(read)
	case protocol.ActionFileMkdir:
		_ = decode(&path)
		return nil, m.Mkdir(path.Path)
	case protocol.ActionFileUploadBegin:
		_ = decode(&begin)
		return m.BeginUpload(begin)
	case protocol.ActionFileUploadWrite:
		_ = decode(&write)
		return m.WriteUpload(write)
	case protocol.ActionFileUploadFinish:
		_ = decode(&id)
		return m.FinishUpload(id.UploadID)
	}
	return nil, errors.New("not answered in this test: " + message.Action)
}

func codeFor(err error) protocol.ErrorCode {
	switch {
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrNoUpload):
		return protocol.ErrorNotFound
	case errors.Is(err, files.ErrExists), errors.Is(err, files.ErrWrongOffset):
		return protocol.ErrorConflict
	case errors.Is(err, files.ErrNotAllowed):
		return protocol.ErrorPolicyDenied
	}
	return protocol.ErrorInvalidRequest
}

func filesPath(f terminalFixture, rest string, query url.Values) string {
	address := "/api/agents/" + f.agentID + "/files" + rest
	if query != nil {
		address += "?" + query.Encode()
	}
	return address
}

func TestFiles(t *testing.T) {
	t.Run("keeps viewers out of the file manager entirely", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, viewer.do(http.MethodGet, filesPath(f, "", url.Values{"path": {"/"}}), nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodGet, filesPath(f, "/download", url.Values{"path": {"/etc/passwd"}}), nil), http.StatusForbidden, "forbidden")
	})

	t.Run("lists a folder and downloads a file bigger than one chunk, whole and from an offset", func(t *testing.T) {
		f := newTerminalFixture(t)
		dir := t.TempDir()
		content := bytes.Repeat([]byte("0123456789abcdef"), protocol.MaxFileChunk/8) // two chunks
		if err := os.WriteFile(filepath.Join(dir, "backup.tar"), content, 0o600); err != nil {
			t.Fatal(err)
		}
		serveFiles(t, f.agent, files.New(nil))

		var listing protocol.FileListResultPayload
		f.admin.browser.do(http.MethodGet, filesPath(f, "", url.Values{"path": {dir}}), nil).decode(t, &listing)
		if len(listing.Entries) != 1 || listing.Entries[0].SizeBytes != uint64(len(content)) {
			t.Errorf("listing = %+v", listing)
		}

		target := filesPath(f, "/download", url.Values{"path": {filepath.Join(dir, "backup.tar")}})
		got := f.admin.browser.do(http.MethodGet, target, nil)
		expect(t, got, http.StatusOK, "")
		if !bytes.Equal(got.body, content) || got.header.Get("Content-Disposition") != `attachment; filename=backup.tar` {
			t.Errorf("got %d bytes, disposition %q", len(got.body), got.header.Get("Content-Disposition"))
		}

		resumed := f.admin.browser.doWith(http.MethodGet, target, nil, map[string]string{"Range": "bytes=1000-"})
		expect(t, resumed, http.StatusPartialContent, "")
		if !bytes.Equal(resumed.body, content[1000:]) {
			t.Errorf("resumed with %d bytes, want %d", len(resumed.body), len(content)-1000)
		}
		if got := auditActions(t, f.server, "file."); !slices.Equal(got, []string{"file.download:success", "file.download:success"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("uploads in pieces, resumes, and refuses a file over the size limit", func(t *testing.T) {
		f := newTerminalFixture(t)
		dir := t.TempDir()
		serveFiles(t, f.agent, files.New(nil))
		content := bytes.Repeat([]byte("x"), 2*protocol.MaxFileChunk+100)
		path := filepath.Join(dir, "upload.bin")

		var state protocol.FileUploadPayload
		begin := map[string]any{"path": path, "sizeBytes": len(content)}
		f.admin.browser.do(http.MethodPost, filesPath(f, "/uploads", nil), begin).decode(t, &state)
		first := f.admin.browser.putRaw(filesPath(f, "/uploads/"+state.UploadID, url.Values{"offset": {"0"}}), content[:600_000])
		expect(t, first, http.StatusOK, "")
		// The connection dropped: beginning again says how much arrived.
		f.admin.browser.do(http.MethodPost, filesPath(f, "/uploads", nil), begin).decode(t, &state)
		if state.ReceivedBytes != 600_000 {
			t.Fatalf("resumed at %d", state.ReceivedBytes)
		}
		rest := f.admin.browser.putRaw(filesPath(f, "/uploads/"+state.UploadID, url.Values{"offset": {"600000"}}), content[600_000:])
		expect(t, rest, http.StatusOK, "")
		expect(t, f.admin.browser.do(http.MethodPost, filesPath(f, "/uploads/"+state.UploadID+"/finish", nil), nil), http.StatusOK, "")
		if saved, _ := os.ReadFile(path); !bytes.Equal(saved, content) {
			t.Errorf("saved %d bytes", len(saved))
		}
		huge := map[string]any{"path": filepath.Join(dir, "huge.iso"), "sizeBytes": int64(11) << 30}
		expect(t, f.admin.browser.do(http.MethodPost, filesPath(f, "/uploads", nil), huge), http.StatusRequestEntityTooLarge, "too_large")
		repeat := f.admin.browser.putRaw(filesPath(f, "/uploads/"+state.UploadID, url.Values{"offset": {"0"}}), []byte("x"))
		expect(t, repeat, http.StatusNotFound, "not_found")
		if got := auditActions(t, f.server, "file."); !slices.Equal(got, []string{"file.upload:success"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("audits a change the agent refused", func(t *testing.T) {
		f := newTerminalFixture(t)
		serveFiles(t, f.agent, files.New(nil))
		dir := t.TempDir()
		expect(t, f.admin.browser.do(http.MethodPost, filesPath(f, "/mkdir", nil), map[string]string{"path": dir}), http.StatusConflict, "conflict")
		expect(t, f.admin.browser.do(http.MethodPost, filesPath(f, "/mkdir", nil), map[string]string{"path": filepath.Join(dir, "new")}), http.StatusNoContent, "")
		if got := auditActions(t, f.server, "file."); !slices.Equal(got, []string{"file.mkdir:failure", "file.mkdir:success"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("refuses to download a folder", func(t *testing.T) {
		f := newTerminalFixture(t)
		serveFiles(t, f.agent, files.New(nil))
		got := f.admin.browser.do(http.MethodGet, filesPath(f, "/download", url.Values{"path": {t.TempDir()}}), nil)
		expect(t, got, http.StatusBadRequest, "invalid_input")
		if !strings.Contains(string(got.body), "only files") {
			t.Errorf("body = %s", got.body)
		}
	})

	t.Run("refuses an upload piece without an offset", func(t *testing.T) {
		f := newTerminalFixture(t)
		got := f.admin.browser.putRaw(filesPath(f, "/uploads/abc", nil), []byte("x"))
		expect(t, got, http.StatusBadRequest, "invalid_input")
		_, _ = io.Copy(io.Discard, bytes.NewReader(got.body))
	})
}

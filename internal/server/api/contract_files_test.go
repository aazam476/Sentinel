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
	"sync"
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
	var text protocol.FileTextPayload
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
	case protocol.ActionFileReadText:
		_ = decode(&path)
		return m.ReadText(path.Path)
	case protocol.ActionFileWriteText:
		_ = decode(&text)
		return m.WriteText(text)
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
		want := []string{"file.download:success", "file.download:success"}
		if got := eventualAuditActions(t, f.server, "file.", want); !slices.Equal(got, want) {
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

	t.Run("answers each request with its own answer, never another's", func(t *testing.T) {
		f := newTerminalFixture(t)
		links, texts := t.TempDir(), t.TempDir()
		serveFiles(t, f.agent, files.New(nil))
		if err := os.Symlink("/etc/hostname", filepath.Join(links, "link")); err != nil {
			t.Fatal(err)
		}
		// Read back, since Windows spells the target its own way.
		target, err := os.Readlink(filepath.Join(links, "link"))
		if err != nil || target == "" {
			t.Fatalf("the link points to %q: %v", target, err)
		}
		for _, name := range []string{"a.txt", "b.txt"} {
			if err := os.WriteFile(filepath.Join(texts, name), []byte("the text of "+name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		list := func(folder string) []protocol.FileEntry {
			var listing protocol.FileListResultPayload
			f.admin.browser.do(http.MethodGet, filesPath(f, "", url.Values{"path": {folder}}), nil).decode(t, &listing)
			return listing.Entries
		}
		// What one listing says about a link must not show up in the next.
		if got := list(links); len(got) != 1 || got[0].Target != target {
			t.Fatalf("the folder with the link lists as %+v, want the target %q", got, target)
		}
		if got := list(texts); len(got) != 2 || got[0].Target != "" || got[1].Target != "" {
			t.Errorf("a folder of plain files lists as %+v, with the link of the folder listed before it", got)
		}

		// Nor may two people reading at once get each other's file.
		var readers sync.WaitGroup
		for _, name := range []string{"a.txt", "b.txt"} {
			readers.Go(func() {
				for range 25 {
					var text protocol.FileTextPayload
					got := f.admin.browser.do(http.MethodGet, filesPath(f, "/text", url.Values{"path": {filepath.Join(texts, name)}}), nil)
					if err := json.Unmarshal(got.body, &text); err != nil || text.Content != "the text of "+name {
						t.Errorf("reading %s got %q (%v)", name, text.Content, err)
						return
					}
				}
			})
		}
		readers.Wait()
	})

	t.Run("opens and saves a text file as large as the editor takes", func(t *testing.T) {
		f := newTerminalFixture(t)
		dir := t.TempDir()
		serveFiles(t, f.agent, files.New(nil))
		path := filepath.Join(dir, "page.html")
		// Quotes, tags and line ends all take more room as JSON.
		line := `<p class="note">A line of the page.</p>` + "\n"
		content := strings.Repeat(line, protocol.MaxTextFile/len(line))
		save := protocol.FileTextPayload{Path: path, Content: content}
		expect(t, f.admin.browser.do(http.MethodPut, filesPath(f, "/text", nil), save), http.StatusOK, "")
		if saved, err := os.ReadFile(path); err != nil || string(saved) != content {
			t.Fatalf("the file holds %d bytes (%v), want %d", len(saved), err, len(content))
		}
		var opened protocol.FileTextPayload
		f.admin.browser.do(http.MethodGet, filesPath(f, "/text", url.Values{"path": {path}}), nil).decode(t, &opened)
		if opened.Content != content {
			t.Errorf("opened %d bytes, want %d", len(opened.Content), len(content))
		}

		// More than the editor takes is refused before the machine is asked,
		// and a body no text file could need isn't read to its end.
		opened.Content = strings.Repeat("a", protocol.MaxTextFile+1)
		expect(t, f.admin.browser.do(http.MethodPut, filesPath(f, "/text", nil), opened), http.StatusRequestEntityTooLarge, "too_large")
		opened.Content = strings.Repeat("a", 7*protocol.MaxTextFile)
		expect(t, f.admin.browser.do(http.MethodPut, filesPath(f, "/text", nil), opened), http.StatusRequestEntityTooLarge, "body_too_large")
		if got := auditActions(t, f.server, "file."); !slices.Equal(got, []string{"file.text.write:success"}) {
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

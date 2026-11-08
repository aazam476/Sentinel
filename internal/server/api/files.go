// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// maxUploadChunk is the most one upload request may carry. The browser
// sends a file in pieces of this size, so a dropped connection costs at
// most one piece.
const maxUploadChunk = 8 << 20

// errorCodeTooLarge: the file is over the file manager's size limit.
const errorCodeTooLarge = "too_large"

// FileUploadRequest starts, or resumes, an upload.
type FileUploadRequest struct {
	Path      string `json:"path"`
	SizeBytes uint64 `json:"sizeBytes"`
	Overwrite bool   `json:"overwrite"`
}

// FilePathRequest names a file or folder.
type FilePathRequest struct {
	Path string `json:"path"`
}

// FileRenameRequest moves or renames a file or folder.
type FileRenameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// addFileRoutes adds the file manager (§9.7), for those who may manage
// files: listing a machine's files says as much as reading them.
func (h *handlers) addFileRoutes(mux *http.ServeMux) {
	files := func(handler func(http.ResponseWriter, *http.Request, auth.Session)) http.HandlerFunc {
		return h.withPermission(auth.PermissionManageFiles, handler)
	}
	const base = "/api/agents/{id}/files"
	mux.HandleFunc("GET "+base, files(h.fileQuery(protocol.ActionFileList, new(protocol.FileListResultPayload))))
	mux.HandleFunc("GET "+base+"/stat", files(h.fileQuery(protocol.ActionFileStat, new(protocol.FileEntry))))
	mux.HandleFunc("GET "+base+"/text", files(h.fileQuery(protocol.ActionFileReadText, new(protocol.FileTextPayload))))
	mux.HandleFunc("PUT "+base+"/text", files(h.handleSaveText))
	mux.HandleFunc("GET "+base+"/download", files(h.handleDownload))
	mux.HandleFunc("POST "+base+"/mkdir", files(h.handleMkdir))
	mux.HandleFunc("POST "+base+"/rename", files(h.handleRename))
	mux.HandleFunc("DELETE "+base, files(h.handleDeleteFile))
	mux.HandleFunc("POST "+base+"/uploads", files(h.handleBeginUpload))
	mux.HandleFunc("PUT "+base+"/uploads/{upload}", files(h.handleUploadChunk))
	mux.HandleFunc("POST "+base+"/uploads/{upload}/finish", files(h.handleFinishUpload))
	mux.HandleFunc("DELETE "+base+"/uploads/{upload}", files(h.handleCancelUpload))
}

// fileQuery returns a handler asking the agent about the path in the
// query string, answering with the agent's answer as it is.
func (h *handlers) fileQuery(action string, result any) func(http.ResponseWriter, *http.Request, auth.Session) {
	return func(w http.ResponseWriter, r *http.Request, session auth.Session) {
		agent, ok := h.onlineMachine(w, r, session)
		if !ok {
			return
		}
		answer := result
		if err := h.ask(r.Context(), agent, action, protocol.FilePathPayload{Path: r.URL.Query().Get("path")}, answer); err != nil {
			h.writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, answer)
	}
}

// fileChange runs a change on the machine, audits it and answers with the
// agent's answer, or 204 when result is nil.
func (h *handlers) fileChange(w http.ResponseWriter, r *http.Request, session auth.Session, action string, payload, result any, details map[string]any) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	err := h.audited(r, session, agent, action, details, func(ctx context.Context) error {
		return h.ask(ctx, agent, action, payload, result)
	})
	switch {
	case err != nil:
		h.writeAgentError(w, err)
	case result == nil:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func (h *handlers) handleSaveText(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request protocol.FileTextPayload
	if !decodeJSON(w, r, &request) {
		return
	}
	h.fileChange(w, r, session, protocol.ActionFileWriteText, request, new(protocol.FileEntry),
		map[string]any{"path": request.Path, "bytes": len(request.Content)})
}

func (h *handlers) handleMkdir(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request FilePathRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	h.fileChange(w, r, session, protocol.ActionFileMkdir, protocol.FilePathPayload(request), nil, map[string]any{"path": request.Path})
}

func (h *handlers) handleRename(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request FileRenameRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	h.fileChange(w, r, session, protocol.ActionFileRename, protocol.FileRenamePayload(request), nil,
		map[string]any{"from": request.From, "to": request.To})
}

func (h *handlers) handleDeleteFile(w http.ResponseWriter, r *http.Request, session auth.Session) {
	request := protocol.FileDeletePayload{Path: r.URL.Query().Get("path"), Recursive: r.URL.Query().Get("recursive") == "true"}
	h.fileChange(w, r, session, protocol.ActionFileDelete, request, nil,
		map[string]any{"path": request.Path, "recursive": request.Recursive})
}

func (h *handlers) handleBeginUpload(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var request FileUploadRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.SizeBytes > uint64(h.maxFileBytes) { //nolint:gosec // G115: the limit is positive
		writeError(w, http.StatusRequestEntityTooLarge, errorCodeTooLarge, h.tooLargeMessage())
		return
	}
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var started protocol.FileUploadPayload
	if err := h.ask(r.Context(), agent, protocol.ActionFileUploadBegin, protocol.FileUploadBeginPayload(request), &started); err != nil {
		h.writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, started)
}

func (h *handlers) tooLargeMessage() string {
	return fmt.Sprintf("Files over %d GB can’t be moved by the file manager on this server.", h.maxFileBytes>>30)
}

// handleUploadChunk passes a piece of the file to the agent, in chunks it
// accepts, starting at the offset in the query string.
func (h *handlers) handleUploadChunk(w http.ResponseWriter, r *http.Request, session auth.Session) {
	offset, err := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 64)
	if err != nil {
		writeInvalidInput(w, "offset", "give the offset the piece starts at")
		return
	}
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxUploadChunk)
	state := protocol.FileUploadPayload{UploadID: r.PathValue("upload"), ReceivedBytes: offset}
	buffer := make([]byte, protocol.MaxFileChunk)
	for {
		read, readErr := io.ReadFull(body, buffer)
		if read > 0 {
			chunk := protocol.FileUploadWritePayload{UploadID: state.UploadID, Offset: state.ReceivedBytes, Data: buffer[:read]}
			if err := h.ask(r.Context(), agent, protocol.ActionFileUploadWrite, chunk, &state); err != nil {
				h.writeAgentError(w, err)
				return
			}
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
		if readErr != nil {
			writeError(w, http.StatusRequestEntityTooLarge, errorCodeTooLarge, "Send the file in pieces of at most 8 MB.")
			return
		}
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *handlers) handleFinishUpload(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var entry protocol.FileEntry
	details := map[string]any{"upload": r.PathValue("upload")}
	err := h.audited(r, session, agent, "file.upload", details, func(ctx context.Context) error {
		err := h.ask(ctx, agent, protocol.ActionFileUploadFinish, protocol.FileUploadIDPayload{UploadID: r.PathValue("upload")}, &entry)
		details["path"], details["bytes"] = entry.Path, entry.SizeBytes
		return err
	})
	if err != nil {
		h.writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (h *handlers) handleCancelUpload(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	if err := h.ask(r.Context(), agent, protocol.ActionFileUploadCancel, protocol.FileUploadIDPayload{UploadID: r.PathValue("upload")}, nil); err != nil {
		h.writeAgentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDownload sends a file, read from the agent a chunk at a time as
// the browser takes it. A Range header resumes a download.
func (h *handlers) handleDownload(w http.ResponseWriter, r *http.Request, session auth.Session) {
	agent, ok := h.onlineMachine(w, r, session)
	if !ok {
		return
	}
	var entry protocol.FileEntry
	if err := h.ask(r.Context(), agent, protocol.ActionFileStat, protocol.FilePathPayload{Path: r.URL.Query().Get("path")}, &entry); err != nil {
		h.writeAgentError(w, err)
		return
	}
	if entry.Type != "file" {
		writeInvalidInput(w, "path", "only files can be downloaded")
		return
	}
	if entry.SizeBytes > uint64(h.maxFileBytes) { //nolint:gosec // G115: the limit is positive
		writeError(w, http.StatusRequestEntityTooLarge, errorCodeTooLarge, h.tooLargeMessage())
		return
	}
	start, end, partial := byteRange(r.Header.Get("Range"), entry.SizeBytes)
	writeDownloadHeaders(w, entry, start, end, partial)
	sent, err := h.sendFile(r.Context(), w, agent, entry, start, end)
	outcome := audit.OutcomeSuccess
	details := map[string]any{"path": entry.Path, "bytes": sent, "from": start}
	if err != nil {
		outcome, details["error"] = audit.OutcomeFailure, err.Error()
	}
	h.auditMachine(context.WithoutCancel(r.Context()), session, agent, requestInfo(r), "file.download", outcome, details)
}

// byteRange reads a single "bytes=start-" or "bytes=start-end" range. A
// missing, malformed or unsatisfiable range means the whole file.
func byteRange(header string, size uint64) (start, end uint64, partial bool) {
	spec, found := strings.CutPrefix(header, "bytes=")
	if !found || size == 0 || strings.Contains(spec, ",") {
		return 0, size, false
	}
	first, last, found := strings.Cut(spec, "-")
	from, err := strconv.ParseUint(first, 10, 64)
	if !found || err != nil || from >= size {
		return 0, size, false
	}
	end = size
	if to, err := strconv.ParseUint(last, 10, 64); err == nil && to+1 < size && to >= from {
		end = to + 1
	}
	return from, end, true
}

func writeDownloadHeaders(w http.ResponseWriter, entry protocol.FileEntry, start, end uint64, partial bool) {
	header := w.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(strings.ReplaceAll(entry.Path, `\`, "/"))}))
	header.Set("Content-Length", strconv.FormatUint(end-start, 10))
	header.Set("Accept-Ranges", "bytes")
	header.Set("Cache-Control", "no-store")
	if partial {
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, entry.SizeBytes))
		w.WriteHeader(http.StatusPartialContent)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// errFileChanged: the file changed while it was being downloaded.
var errFileChanged = errors.New("the file changed while it was being downloaded")

// sendFile copies the file from start to end, one chunk per request. It
// stops if the file changes underneath it, rather than send a mixture.
func (h *handlers) sendFile(ctx context.Context, w http.ResponseWriter, agent fleet.Agent, entry protocol.FileEntry, start, end uint64) (uint64, error) {
	sent := uint64(0)
	for offset := start; offset < end; {
		var chunk protocol.FileChunkPayload
		length := uint32(min(end-offset, protocol.MaxFileChunk))
		if err := h.ask(ctx, agent, protocol.ActionFileRead, protocol.FileReadPayload{Path: entry.Path, Offset: offset, Length: length}, &chunk); err != nil {
			return sent, err
		}
		if chunk.Modified != entry.Modified || chunk.SizeBytes != entry.SizeBytes || len(chunk.Data) == 0 {
			return sent, errFileChanged
		}
		if _, err := w.Write(chunk.Data); err != nil {
			return sent, fmt.Errorf("send to the browser: %w", err)
		}
		offset += uint64(len(chunk.Data))
		sent += uint64(len(chunk.Data))
	}
	return sent, nil
}

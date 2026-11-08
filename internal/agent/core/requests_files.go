// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"

	"github.com/aazam476/sentinel/internal/agent/files"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Files is the file manager (internal/agent/files).
type Files interface {
	List(path string) (protocol.FileListResultPayload, error)
	Stat(path string) (protocol.FileEntry, error)
	Read(request protocol.FileReadPayload) (protocol.FileChunkPayload, error)
	Mkdir(path string) error
	Rename(request protocol.FileRenamePayload) error
	Delete(request protocol.FileDeletePayload) error
	ReadText(path string) (protocol.FileTextPayload, error)
	WriteText(request protocol.FileTextPayload) (protocol.FileEntry, error)
	BeginUpload(request protocol.FileUploadBeginPayload) (protocol.FileUploadPayload, error)
	WriteUpload(request protocol.FileUploadWritePayload) (protocol.FileUploadPayload, error)
	FinishUpload(id string) (protocol.FileEntry, error)
	CancelUpload(id string) error
}

// fileHandlers answers the file actions (§9.7). The local policy can switch
// the file manager off, or limit it to some folders (SEC-22, SEC-25).
func (c *connection) fileHandlers() map[string]handler {
	manager := func() Files { return c.agent.options.Files }
	byPath := func(do func(path string) (any, error)) handler {
		return fileCall(c, func(p protocol.FilePathPayload) (any, error) { return do(p.Path) })
	}
	return map[string]handler{
		protocol.ActionFileList:     byPath(func(path string) (any, error) { return manager().List(path) }),
		protocol.ActionFileStat:     byPath(func(path string) (any, error) { return manager().Stat(path) }),
		protocol.ActionFileReadText: byPath(func(path string) (any, error) { return manager().ReadText(path) }),
		protocol.ActionFileMkdir:    byPath(func(path string) (any, error) { return nil, manager().Mkdir(path) }),
		protocol.ActionFileRead: fileCall(c, func(p protocol.FileReadPayload) (any, error) {
			return manager().Read(p)
		}),
		protocol.ActionFileRename: fileCall(c, func(p protocol.FileRenamePayload) (any, error) {
			return nil, manager().Rename(p)
		}),
		protocol.ActionFileDelete: fileCall(c, func(p protocol.FileDeletePayload) (any, error) {
			return nil, manager().Delete(p)
		}),
		protocol.ActionFileWriteText: fileCall(c, func(p protocol.FileTextPayload) (any, error) {
			return manager().WriteText(p)
		}),
		protocol.ActionFileUploadBegin: fileCall(c, func(p protocol.FileUploadBeginPayload) (any, error) {
			return manager().BeginUpload(p)
		}),
		protocol.ActionFileUploadWrite: fileCall(c, func(p protocol.FileUploadWritePayload) (any, error) {
			return manager().WriteUpload(p)
		}),
		protocol.ActionFileUploadFinish: fileCall(c, func(p protocol.FileUploadIDPayload) (any, error) {
			return manager().FinishUpload(p.UploadID)
		}),
		protocol.ActionFileUploadCancel: fileCall(c, func(p protocol.FileUploadIDPayload) (any, error) {
			return nil, manager().CancelUpload(p.UploadID)
		}),
	}
}

// fileCall is the handler for a file action with a payload of type P.
func fileCall[P any](c *connection, run func(payload P) (any, error)) handler {
	return func(_ context.Context, request protocol.Message) (any, *protocol.Error) {
		if refusal := c.refuseUnlessAllowed(policy.Files, "The file manager"); refusal != nil {
			return nil, refusal
		}
		var payload P
		if err := protocol.DecodePayload(request, &payload); err != nil {
			return nil, invalidRequest(err)
		}
		result, err := run(payload)
		if err != nil {
			return nil, c.fileError(err)
		}
		return result, nil
	}
}

func (c *connection) fileError(err error) *protocol.Error {
	code := protocol.ErrorInternal
	switch {
	case errors.Is(err, files.ErrNotAllowed):
		code = protocol.ErrorPolicyDenied
	case errors.Is(err, files.ErrNotFound), errors.Is(err, files.ErrNoUpload):
		code = protocol.ErrorNotFound
	case errors.Is(err, files.ErrExists), errors.Is(err, files.ErrChanged), errors.Is(err, files.ErrNotEmpty),
		errors.Is(err, files.ErrWrongOffset), errors.Is(err, files.ErrIncomplete):
		code = protocol.ErrorConflict
	case errors.Is(err, files.ErrInvalidPath), errors.Is(err, files.ErrProtected), errors.Is(err, files.ErrNotText),
		errors.Is(err, files.ErrIsFolder), errors.Is(err, files.ErrNotFolder), errors.Is(err, files.ErrTooLong):
		code = protocol.ErrorInvalidRequest
	default:
		c.agent.options.Logger.Warn("a file request failed", "error", err)
	}
	return &protocol.Error{Code: code, Message: err.Error()}
}

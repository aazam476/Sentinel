// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/files"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// startWithFiles runs an agent whose file manager works in a temporary
// folder, under localPolicy.
func startWithFiles(t *testing.T, policyText func(dir string) string) (*server, string, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	localPolicy, err := policy.Parse(policyText(dir))
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(t)
	s.machine.Files = files.New(localPolicy.Files.AllowedPaths)
	events := s.subscribe()
	agent := s.startAgent(localPolicy)
	waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
	return s, agent.state.AgentID, dir
}

func codeOf(err error) protocol.ErrorCode {
	var agentErr *protocol.Error
	if errors.As(err, &agentErr) {
		return agentErr.Code
	}
	return ""
}

func TestFileRequests(t *testing.T) {
	t.Run("uploads, lists, reads, edits, renames and deletes", func(t *testing.T) {
		s, agentID, dir := startWithFiles(t, func(string) string { return "" })
		path := filepath.Join(dir, "hello.txt")
		var upload protocol.FileUploadPayload
		if err := s.request(t, agentID, protocol.ActionFileUploadBegin, protocol.FileUploadBeginPayload{Path: path, SizeBytes: 5}, &upload); err != nil {
			t.Fatal(err)
		}
		if err := s.request(t, agentID, protocol.ActionFileUploadWrite, protocol.FileUploadWritePayload{UploadID: upload.UploadID, Data: []byte("hello")}, &upload); err != nil || upload.ReceivedBytes != 5 {
			t.Fatalf("got %+v, %v", upload, err)
		}
		if err := s.request(t, agentID, protocol.ActionFileUploadFinish, protocol.FileUploadIDPayload{UploadID: upload.UploadID}, nil); err != nil {
			t.Fatal(err)
		}
		var listing protocol.FileListResultPayload
		if err := s.request(t, agentID, protocol.ActionFileList, protocol.FilePathPayload{Path: dir}, &listing); err != nil || len(listing.Entries) != 1 {
			t.Fatalf("got %+v, %v", listing, err)
		}
		var chunk protocol.FileChunkPayload
		if err := s.request(t, agentID, protocol.ActionFileRead, protocol.FileReadPayload{Path: path, Length: 4}, &chunk); err != nil || string(chunk.Data) != "hell" {
			t.Fatalf("got %+v, %v", chunk, err)
		}
		var text protocol.FileTextPayload
		if err := s.request(t, agentID, protocol.ActionFileReadText, protocol.FilePathPayload{Path: path}, &text); err != nil {
			t.Fatal(err)
		}
		text.Content = "hello, world"
		if err := s.request(t, agentID, protocol.ActionFileWriteText, text, nil); err != nil {
			t.Fatal(err)
		}
		folder := filepath.Join(dir, "old")
		renamed := filepath.Join(dir, "new")
		for _, step := range []struct {
			action  string
			payload any
		}{
			{protocol.ActionFileMkdir, protocol.FilePathPayload{Path: folder}},
			{protocol.ActionFileRename, protocol.FileRenamePayload{From: folder, To: renamed}},
			{protocol.ActionFileStat, protocol.FilePathPayload{Path: renamed}},
			{protocol.ActionFileDelete, protocol.FileDeletePayload{Path: renamed}},
		} {
			if err := s.request(t, agentID, step.action, step.payload, nil); err != nil {
				t.Fatalf("%s: %v", step.action, err)
			}
		}
		if data, _ := os.ReadFile(path); string(data) != "hello, world" {
			t.Errorf("file holds %q", data)
		}
	})

	failures := []struct {
		name    string
		action  string
		payload func(dir string) any
		want    protocol.ErrorCode
	}{
		{"says when a file isn't there", protocol.ActionFileStat, func(dir string) any {
			return protocol.FilePathPayload{Path: filepath.Join(dir, "nothing")}
		}, protocol.ErrorNotFound},
		{"refuses a relative path", protocol.ActionFileList, func(string) any {
			return protocol.FilePathPayload{Path: "etc"}
		}, protocol.ErrorInvalidRequest},
		{"refuses to replace a file", protocol.ActionFileUploadBegin, func(dir string) any {
			return protocol.FileUploadBeginPayload{Path: dir, SizeBytes: 1}
		}, protocol.ErrorConflict},
		{"refuses a payload that isn't valid", protocol.ActionFileRead, func(string) any { return "x" }, protocol.ErrorInvalidRequest},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			s, agentID, dir := startWithFiles(t, func(string) string { return "" })
			if got := codeOf(s.request(t, agentID, tc.action, tc.payload(dir), nil)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("SEC-22 refuses every file action when the local policy switches the file manager off", func(t *testing.T) {
		s, agentID, dir := startWithFiles(t, func(string) string { return "[capabilities]\nfiles = false\n" })
		for _, action := range []string{protocol.ActionFileList, protocol.ActionFileRead, protocol.ActionFileUploadBegin, protocol.ActionFileDelete} {
			if got := codeOf(s.request(t, agentID, action, protocol.FilePathPayload{Path: dir}, nil)); got != protocol.ErrorPolicyDenied {
				t.Errorf("%s: got %q", action, got)
			}
		}
	})

	t.Run("SEC-25 refuses paths outside the folders the local policy allows", func(t *testing.T) {
		s, agentID, dir := startWithFiles(t, func(dir string) string {
			if err := os.Mkdir(filepath.Join(dir, "srv"), 0o750); err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("[files]\nallowed_paths = [%q]\n", filepath.Join(dir, "srv"))
		})
		if got := codeOf(s.request(t, agentID, protocol.ActionFileList, protocol.FilePathPayload{Path: dir}, nil)); got != protocol.ErrorPolicyDenied {
			t.Errorf("got %q", got)
		}
		if err := s.request(t, agentID, protocol.ActionFileList, protocol.FilePathPayload{Path: filepath.Join(dir, "srv")}, nil); err != nil {
			t.Errorf("inside: %v", err)
		}
	})
}

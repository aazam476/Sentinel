// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/files"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// tooMuchText is a file manager whose text file is more than one message
// carries. It stands for any answer that grew too big: a log, a listing.
type tooMuchText struct{ core.Files }

func (tooMuchText) ReadText(path string) (protocol.FileTextPayload, error) {
	return protocol.FileTextPayload{Path: path, Content: strings.Repeat("a", protocol.MaxMessageSize)}, nil
}

// wentOffline reports whether the events so far say the machine's
// connection ended.
func wentOffline(events <-chan hub.Event, agentID string) bool {
	for {
		select {
		case event := <-events:
			if event.PublicID == agentID && event.Kind == hub.EventOffline {
				return true
			}
		default:
			return false
		}
	}
}

func TestLargeAnswers(t *testing.T) {
	t.Run("opens and saves the largest text file the editor takes, however it encodes", func(t *testing.T) {
		s, agentID, dir := startWithFiles(t, func(string) string { return "" })
		events := s.subscribe()
		path := filepath.Join(dir, "page.html")
		// JSON writes each < as six bytes, so this megabyte travels as six.
		if err := os.WriteFile(path, []byte(strings.Repeat("<", protocol.MaxTextFile)), 0o600); err != nil {
			t.Fatal(err)
		}
		var text protocol.FileTextPayload
		if err := s.request(t, agentID, protocol.ActionFileReadText, protocol.FilePathPayload{Path: path}, &text); err != nil || len(text.Content) != protocol.MaxTextFile {
			t.Fatalf("read %d bytes: %v", len(text.Content), err)
		}
		text.Content = strings.Repeat(">", protocol.MaxTextFile)
		if err := s.request(t, agentID, protocol.ActionFileWriteText, text, nil); err != nil {
			t.Fatalf("save: %v", err)
		}
		if saved, err := os.ReadFile(path); err != nil || string(saved) != text.Content {
			t.Errorf("the file holds %d bytes (%v), not what was saved", len(saved), err)
		}
		if wentOffline(events, agentID) {
			t.Error("the machine's connection was dropped over a file the editor takes")
		}
	})

	t.Run("says an answer is too large to send, and stays connected", func(t *testing.T) {
		localPolicy, err := policy.Parse("")
		if err != nil {
			t.Fatal(err)
		}
		s := newServer(t)
		s.machine.Files = tooMuchText{files.New(nil)}
		events := s.subscribe()
		agent := s.startAgent(localPolicy)
		agentID := agent.state.AgentID
		waitFor(t, events, agentID, hub.EventInfoUpdated)
		folder := t.TempDir()

		err = s.request(t, agentID, protocol.ActionFileReadText, protocol.FilePathPayload{Path: filepath.Join(folder, "huge.log")}, nil)
		if got := codeOf(err); got != protocol.ErrorTooLarge {
			t.Fatalf("got %q (%v), want %q", got, err, protocol.ErrorTooLarge)
		}
		// Everything else on the connection carries on: a terminal, a
		// transfer, the next request.
		if err := s.request(t, agentID, protocol.ActionFileStat, protocol.FilePathPayload{Path: folder}, nil); err != nil {
			t.Errorf("the next request failed: %v", err)
		}
		if wentOffline(events, agentID) {
			t.Error("the machine's connection was dropped over one answer")
		}
	})
}

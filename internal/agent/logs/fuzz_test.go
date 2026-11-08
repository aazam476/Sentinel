// SPDX-License-Identifier: Apache-2.0

package logs_test

import (
	"os"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/logs"
	"github.com/aazam476/sentinel/internal/protocol"
)

// fuzzParser checks parse never panics and only returns entries with a
// level the UI knows, starting from the golden files given.
func fuzzParser(f *testing.F, parse func([]byte) []protocol.LogEntry, seeds ...string) {
	for _, seed := range seeds {
		data, err := os.ReadFile("testdata/" + seed)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, output []byte) {
		for _, entry := range parse(output) {
			switch entry.Level {
			case protocol.LogError, protocol.LogWarning, protocol.LogNotice, protocol.LogInfo, protocol.LogDebug:
			default:
				t.Errorf("unknown level %q", entry.Level)
			}
		}
	})
}

func FuzzParseJournal(f *testing.F) { fuzzParser(f, logs.ParseJournal, "journald-255.jsonl") }

func FuzzParseUnifiedLog(f *testing.F) {
	fuzzParser(f, logs.ParseUnifiedLog, "macos-14-log-show.ndjson")
}

func FuzzParseEvents(f *testing.F) {
	fuzzParser(f, logs.ParseEvents, "windows-2022-application.xml")
}

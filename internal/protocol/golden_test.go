// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
)

// goldenPayloads maps each golden fixture to the payload type it carries,
// or nil for messages without a payload.
func goldenPayloads() map[string]func() any {
	return map[string]func() any{
		"request-system-info.json":            nil,
		"response-system-info.json":           func() any { return &protocol.SystemInfo{} },
		"response-error.json":                 nil,
		"event-metrics-sample.json":           func() any { return &protocol.MetricsSample{} },
		"stream-close.json":                   func() any { return &protocol.StreamClosePayload{} },
		"ping.json":                           nil,
		"pong.json":                           nil,
		"enroll.json":                         func() any { return &protocol.EnrollPayload{} },
		"enrolled.json":                       func() any { return &protocol.EnrolledPayload{} },
		"hello.json":                          func() any { return &protocol.HelloPayload{} },
		"challenge.json":                      func() any { return &protocol.ChallengePayload{} },
		"prove.json":                          func() any { return &protocol.ProvePayload{} },
		"welcome.json":                        func() any { return &protocol.WelcomePayload{} },
		"request-terminal-open.json":          func() any { return &protocol.TerminalOpenPayload{} },
		"response-terminal-opened.json":       func() any { return &protocol.TerminalOpenedPayload{} },
		"request-terminal-resize.json":        func() any { return &protocol.TerminalResizePayload{} },
		"request-service-list.json":           nil,
		"response-service-list.json":          func() any { return &protocol.ServiceListPayload{} },
		"request-service-restart.json":        func() any { return &protocol.ServiceActionPayload{} },
		"request-log-query.json":              func() any { return &protocol.LogQueryPayload{} },
		"response-log-entries.json":           func() any { return &protocol.LogEntriesPayload{} },
		"request-process-list.json":           nil,
		"response-process-list.json":          func() any { return &protocol.ProcessListPayload{} },
		"request-process-kill.json":           func() any { return &protocol.ProcessKillPayload{} },
		"request-system-reboot.json":          nil,
		"request-docker-container-list.json":  nil,
		"response-docker-container-list.json": func() any { return &protocol.ContainerListPayload{} },
		"request-docker-container-logs.json":  func() any { return &protocol.ContainerLogsPayload{} },
		"response-docker-container-logs.json": func() any { return &protocol.ContainerLogsResultPayload{} },
		"request-docker-image-pull.json":      func() any { return &protocol.ImageRefPayload{} },
		"response-docker-unavailable.json":    nil,
	}
}

func TestGoldenFixtures(t *testing.T) {
	directory := filepath.Join("..", "..", "testdata", "protocol")
	files, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	payloads := goldenPayloads()
	if len(files) != len(payloads) {
		t.Fatalf("found %d fixtures and %d expected; every fixture needs an entry in goldenPayloads", len(files), len(payloads))
	}

	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			newPayload, known := payloads[name]
			if !known {
				t.Fatalf("no expected payload type for %s", name)
			}

			message, err := protocol.Decode(data)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if newPayload != nil {
				if err := protocol.DecodePayload(message, newPayload()); err != nil {
					t.Fatalf("DecodePayload: %v", err)
				}
			}
			assertRoundTrip(t, message, data)
		})
	}
}

// assertRoundTrip checks that encoding the decoded message gives back the
// same JSON, so nothing is lost or renamed on either side.
func assertRoundTrip(t *testing.T, message protocol.Message, original []byte) {
	t.Helper()
	encoded, err := protocol.Encode(message)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var want, got bytes.Buffer
	if err := json.Compact(&want, original); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&got, encoded); err != nil {
		t.Fatal(err)
	}
	if want.String() != got.String() {
		t.Errorf("round trip changed the message:\n got  %s\n want %s", got.String(), want.String())
	}
}

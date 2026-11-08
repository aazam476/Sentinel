// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

const exampleID = "01JBX3K5Q7Z8M4N6P8R0T2V4W6"

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{name: "accepts a request", data: `{"type":"request","id":"` + exampleID + `","action":"docker.container.restart","payload":{"containerId":"abc"}}`},
		{name: "accepts a ping", data: `{"type":"ping"}`},
		{name: "refuses an unknown type", data: `{"type":"shout"}`, wantErr: "unknown message type"},
		{name: "refuses a request without an ID", data: `{"type":"request","action":"system.info"}`, wantErr: "invalid ID"},
		{name: "refuses a response with a malformed ID", data: `{"type":"response","id":"not-a-ulid"}`, wantErr: "invalid ID"},
		{name: "refuses a request without an action", data: `{"type":"request","id":"` + exampleID + `"}`, wantErr: "invalid action"},
		{name: "refuses an action with uppercase letters", data: `{"type":"event","id":"` + exampleID + `","action":"Metrics.Sample"}`, wantErr: "invalid action"},
		{name: "refuses a one-word action", data: `{"type":"event","id":"` + exampleID + `","action":"metrics"}`, wantErr: "invalid action"},
		{name: "refuses unknown fields", data: `{"type":"ping","extra":1}`, wantErr: "unknown field"},
		{name: "refuses data after the message", data: `{"type":"ping"}{"type":"pong"}`, wantErr: "unexpected data"},
		{name: "refuses something that isn't JSON", data: `ping`, wantErr: "decode message"},
		{name: "refuses a message over 1 MiB", data: `{"type":"ping","payload":"` + strings.Repeat("a", protocol.MaxMessageSize) + `"}`, wantErr: "limit"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := protocol.Decode([]byte(test.data))
			if test.wantErr == "" {
				if err != nil {
					t.Errorf("Decode returned %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("Decode error = %v, want one containing %q", err, test.wantErr)
			}
		})
	}
}

func TestEncode(t *testing.T) {
	t.Run("refuses an invalid message", func(t *testing.T) {
		if _, err := protocol.Encode(protocol.Message{Type: protocol.TypeRequest}); err == nil {
			t.Error("Encode accepted a request without an ID or action")
		}
	})
}

func TestPayloads(t *testing.T) {
	t.Run("round-trips a payload", func(t *testing.T) {
		message, err := protocol.WithPayload(protocol.Message{Type: protocol.TypeWelcome}, protocol.WelcomePayload{HeartbeatSeconds: 15})
		if err != nil {
			t.Fatal(err)
		}
		var welcome protocol.WelcomePayload
		if err := protocol.DecodePayload(message, &welcome); err != nil || welcome.HeartbeatSeconds != 15 {
			t.Errorf("DecodePayload = %+v, %v", welcome, err)
		}
	})

	t.Run("refuses a missing payload", func(t *testing.T) {
		if err := protocol.DecodePayload(protocol.Message{Type: protocol.TypeHello}, &protocol.HelloPayload{}); err == nil {
			t.Error("DecodePayload accepted a message without a payload")
		}
	})

	t.Run("refuses unknown payload fields", func(t *testing.T) {
		message := protocol.Message{Type: protocol.TypeWelcome, Payload: []byte(`{"heartbeatSeconds":15,"admin":true}`)}
		if err := protocol.DecodePayload(message, &protocol.WelcomePayload{}); err == nil {
			t.Error("DecodePayload accepted an unknown field")
		}
	})

	t.Run("reports a payload that can't be encoded", func(t *testing.T) {
		if _, err := protocol.WithPayload(protocol.Message{Type: protocol.TypePing}, make(chan int)); err == nil {
			t.Error("WithPayload accepted a channel")
		}
	})

	t.Run("an error reads as code and message", func(t *testing.T) {
		err := &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "terminal is disabled"}
		if err.Error() != "policy_denied: terminal is disabled" {
			t.Errorf("Error() = %q", err.Error())
		}
	})
}

func TestIDs(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

	t.Run("makes valid, unique IDs", func(t *testing.T) {
		seen := map[string]bool{}
		for range 1000 {
			id := protocol.NewID(now)
			if !protocol.IsID(id) {
				t.Fatalf("NewID returned invalid ID %q", id)
			}
			if seen[id] {
				t.Fatalf("NewID repeated %q", id)
			}
			seen[id] = true
		}
	})

	t.Run("sorts IDs by time", func(t *testing.T) {
		earlier := protocol.NewID(now)
		later := protocol.NewID(now.Add(time.Millisecond))
		if earlier[:10] >= later[:10] {
			t.Errorf("time part of %s is not before %s", earlier, later)
		}
	})

	t.Run("encodes the time in the first 10 characters", func(t *testing.T) {
		// 2026-11-07T12:00:00Z is 1794052800000 ms = 0x01A1 0x0E3E 0x9300.
		if got := protocol.NewID(now)[:10]; got != "01M6TY2FG0" {
			t.Errorf("time part = %s, want 01M6TY2FG0", got)
		}
	})

	tests := []struct {
		id   string
		want bool
	}{
		{exampleID, true},
		{"01JBX3K5Q7Z8M4N6P8R0T2V4W", false},
		{"01JBX3K5Q7Z8M4N6P8R0T2V4W66", false},
		{"81JBX3K5Q7Z8M4N6P8R0T2V4W6", false},
		{"01JBX3K5Q7Z8M4N6P8R0T2V4WU", false},
		{"01jbx3k5q7z8m4n6p8r0t2v4w6", false},
	}
	for _, test := range tests {
		if got := protocol.IsID(test.id); got != test.want {
			t.Errorf("IsID(%q) = %v, want %v", test.id, got, test.want)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"type":"request","id":"` + exampleID + `","action":"system.info","payload":{}}`))
	f.Add([]byte(`{"type":"ping"}`))
	f.Add([]byte(`{"type":"response","id":"` + exampleID + `","error":{"code":"internal","message":"x"}}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		message, err := protocol.Decode(data)
		if err != nil {
			return
		}
		// Anything Decode accepts must encode again and decode to the same
		// type, ID and action.
		encoded, err := protocol.Encode(message)
		if err != nil {
			t.Fatalf("Decode accepted %q but Encode refused it: %v", data, err)
		}
		again, err := protocol.Decode(encoded)
		if err != nil || again.Type != message.Type || again.ID != message.ID || again.Action != message.Action {
			t.Fatalf("round trip of %q changed the message", data)
		}
	})
}

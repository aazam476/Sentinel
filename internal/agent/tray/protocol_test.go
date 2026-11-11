// SPDX-License-Identifier: Apache-2.0

package tray_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

func sampleStatus() *tray.Status {
	return &tray.Status{
		Connected: true, Server: "https://sentinel.example.com", Version: "v1.2.0", ConsentRequired: true,
		Sessions: []tray.Session{{ID: "s1", Kind: tray.KindDesktop, User: "admin", Since: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)}},
	}
}

func TestMessagesRoundTrip(t *testing.T) {
	for _, message := range []tray.Message{
		{Type: tray.TypeStatus, Status: sampleStatus()},
		{Type: tray.TypeAsk, ID: "q1", Question: "admin wants to see this screen through Sentinel. Allow it?"},
		{Type: tray.TypeAsked, ID: "q1"},
		{Type: tray.TypeAnswer, ID: "q1", Allow: true},
		{Type: tray.TypeEnd, ID: "s1"},
		{Type: tray.TypePause, Paused: true},
	} {
		t.Run(message.Type, func(t *testing.T) {
			line, err := tray.Encode(message)
			if err != nil {
				t.Fatal(err)
			}
			got, err := tray.NewReader(bytes.NewReader(line)).Read()
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			again, err := tray.Encode(got)
			if err != nil || !bytes.Equal(again, line) {
				t.Errorf("round trip = %q, %v; want %q", again, err, line)
			}
		})
	}
}

func TestDecodeRefusesWhatItCantShowSafely(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{name: "refuses an unknown type", line: `{"type":"format-disk"}`},
		{name: "refuses unknown fields", line: `{"type":"pause","paused":true,"extra":1}`},
		{name: "refuses a status without a status", line: `{"type":"status"}`},
		{name: "refuses an ask without a question", line: `{"type":"ask","id":"q1"}`},
		{name: "refuses an answer without an ID", line: `{"type":"answer","allow":true}`},
		{name: "refuses control characters in a question", line: `{"type":"ask","id":"q1","question":"Allow?\u001b[2J"}`},
		{name: "refuses a session of an unknown kind", line: `{"type":"status","status":{"connected":true,"server":"","version":"","paused":false,"consentRequired":false,"sessions":[{"id":"s","kind":"files","user":"","since":"2026-11-11T09:00:00Z"}]}}`},
		{name: "refuses a session without an ID", line: `{"type":"status","status":{"connected":true,"server":"","version":"","paused":false,"consentRequired":false,"sessions":[{"id":"","kind":"terminal","user":"","since":"2026-11-11T09:00:00Z"}]}}`},
		{name: "refuses two values on one line", line: `{"type":"pause"}{"type":"pause"}`},
		{name: "refuses a question too long for a dialog", line: `{"type":"ask","id":"q1","question":"` + strings.Repeat("a", 600) + `"}`},
		{name: "refuses invalid JSON", line: `{"type":`},
		{name: "refuses a server name with control characters", line: `{"type":"status","status":{"connected":true,"server":"a\u0000b","version":"","paused":false,"consentRequired":false,"sessions":[]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := tray.Decode([]byte(test.line)); !errors.Is(err, tray.ErrBadMessage) {
				t.Errorf("Decode error = %v, want ErrBadMessage", err)
			}
		})
	}
}

func TestReaderLimitsLineLength(t *testing.T) {
	_, err := tray.NewReader(strings.NewReader(strings.Repeat("x", 70<<10) + "\n")).Read()
	if err == nil || errors.Is(err, io.EOF) {
		t.Errorf("Read of a 70 KiB line = %v, want an error", err)
	}
	if _, err := tray.NewReader(strings.NewReader("")).Read(); !errors.Is(err, io.EOF) {
		t.Errorf("Read at the end = %v, want io.EOF", err)
	}
}

func TestEncodeRefusesAMessageTooLargeToRead(t *testing.T) {
	status := sampleStatus()
	status.Server = strings.Repeat("a", 70<<10)
	if _, err := tray.Encode(tray.Message{Type: tray.TypeStatus, Status: status}); !errors.Is(err, tray.ErrBadMessage) {
		t.Errorf("Encode error = %v, want ErrBadMessage", err)
	}
}

func FuzzDecode(f *testing.F) {
	for _, message := range []tray.Message{
		{Type: tray.TypeStatus, Status: sampleStatus()},
		{Type: tray.TypeAsk, ID: "q1", Question: "Allow it?"},
		{Type: tray.TypeAnswer, ID: "q1", Allow: true},
	} {
		line, err := tray.Encode(message)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(bytes.TrimSuffix(line, []byte("\n")))
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		message, err := tray.Decode(line)
		if err != nil {
			return
		}
		// Whatever is accepted encodes again and decodes to the same.
		again, err := tray.Encode(message)
		if err != nil {
			t.Fatalf("accepted %q but can't encode it: %v", line, err)
		}
		if _, err := tray.Decode(bytes.TrimSuffix(again, []byte("\n"))); err != nil {
			t.Fatalf("accepted %q but not its encoding %q: %v", line, again, err)
		}
	})
}

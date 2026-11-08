// SPDX-License-Identifier: Apache-2.0

package logs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Journald reads journald through journalctl.
type Journald struct {
	runner command.Runner
	clock  Clock
}

// Query asks journalctl for one more entry than the limit, newest first,
// to know whether there were more.
func (j Journald) Query(ctx context.Context, request protocol.LogQueryPayload) (protocol.LogEntriesPayload, error) {
	q, err := check("linux", request, j.clock.Now())
	if err != nil {
		return protocol.LogEntriesPayload{}, err
	}
	args := []string{"--output=json", "--no-pager", "--reverse", "--lines=" + strconv.Itoa(q.limit+1),
		"--since=@" + strconv.FormatInt(q.since.Unix(), 10)}
	if q.minLevel != "" {
		args = append(args, "--priority="+strconv.Itoa(syslogPriority(q.minLevel)))
	}
	if q.service != "" {
		args = append(args, "--unit="+q.service)
	}
	output, err := j.runner.Run(ctx, "journalctl", args...)
	if err != nil {
		return protocol.LogEntriesPayload{}, fmt.Errorf("journalctl: %w", unsupportedIfMissing(err))
	}
	return newestFirst(ParseJournal(output), q.limit), nil
}

// syslogPriority is the least severe syslog priority at level.
func syslogPriority(level protocol.LogLevel) int {
	switch level {
	case protocol.LogError:
		return 3
	case protocol.LogWarning:
		return 4
	case protocol.LogNotice:
		return 5
	case protocol.LogInfo:
		return 6
	default:
		return 7
	}
}

// journalEntry holds the fields of `journalctl -o json` the agent uses.
// Every value is a string, except MESSAGE, which is an array of bytes when
// the message isn't valid UTF-8, and null when it is too large.
type journalEntry struct {
	Time       string          `json:"__REALTIME_TIMESTAMP"`
	Priority   string          `json:"PRIORITY"`
	Message    json.RawMessage `json:"MESSAGE"`
	Identifier string          `json:"SYSLOG_IDENTIFIER"`
	Command    string          `json:"_COMM"`
	Unit       string          `json:"_SYSTEMD_UNIT"`
}

// ParseJournal reads `journalctl -o json`: one JSON object per line. Lines
// that aren't entries, such as a line cut off at the end, are skipped.
func ParseJournal(output []byte) []protocol.LogEntry {
	var entries []protocol.LogEntry
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var raw journalEntry
		if json.Unmarshal(scanner.Bytes(), &raw) != nil {
			continue
		}
		micros, err := strconv.ParseInt(raw.Time, 10, 64)
		if err != nil {
			continue
		}
		entries = append(entries, protocol.LogEntry{Time: micros / 1000, Level: journalLevel(raw.Priority),
			Source: firstOf(raw.Identifier, raw.Command, raw.Unit), Message: journalMessage(raw.Message)})
	}
	return entries
}

func journalLevel(priority string) protocol.LogLevel {
	switch priority {
	case "0", "1", "2", "3":
		return protocol.LogError
	case "4":
		return protocol.LogWarning
	case "5":
		return protocol.LogNotice
	case "7":
		return protocol.LogDebug
	default:
		return protocol.LogInfo
	}
}

func journalMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "(message too large to show)"
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	// A JSON array decodes into []byte element by element, each checked to
	// fit in a byte.
	var data []byte
	if json.Unmarshal(raw, &data) == nil {
		return string(bytes.ToValidUTF8(data, []byte("\uFFFD")))
	}
	return "(message too large to show)"
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

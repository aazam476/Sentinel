// SPDX-License-Identifier: Apache-2.0

package logs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// unifiedTime is the format of `log show` timestamps and of its --start.
const unifiedTime = "2006-01-02 15:04:05.000000-0700"

// UnifiedLog reads macOS's unified log through `log show`.
type UnifiedLog struct {
	runner command.Runner
	clock  Clock
}

// Query runs `log show` from the start time onwards. It has no limit or
// newest-first option, so the agent keeps the newest entries itself; a
// busy machine's hour of logs can be large, which is why the default
// window is only an hour.
func (u UnifiedLog) Query(ctx context.Context, request protocol.LogQueryPayload) (protocol.LogEntriesPayload, error) {
	q, err := check("darwin", request, u.clock.Now())
	if err != nil {
		return protocol.LogEntriesPayload{}, err
	}
	args := []string{"show", "--style", "ndjson", "--start", q.since.UTC().Format("2006-01-02 15:04:05-0700")}
	if includes(q.minLevel, protocol.LogInfo) {
		args = append(args, "--info")
	}
	if includes(q.minLevel, protocol.LogDebug) {
		args = append(args, "--debug")
	}
	if predicate := unifiedPredicate(q); predicate != "" {
		args = append(args, "--predicate", predicate)
	}
	output, err := u.runner.Run(ctx, "log", args...)
	if err != nil {
		return protocol.LogEntriesPayload{}, fmt.Errorf("log show: %w", unsupportedIfMissing(err))
	}
	return newestFirst(ParseUnifiedLog(output), q.limit), nil
}

// unifiedPredicate filters by level and service. A launchd job logs under
// its label as subsystem if it uses os_log, and otherwise as its process,
// which usually matches the label's last part (com.openssh.sshd → sshd).
// The label was validated, so it has no quotes to break out with.
func unifiedPredicate(q query) string {
	var parts []string
	if q.minLevel == protocol.LogError || q.minLevel == protocol.LogWarning {
		parts = append(parts, "(messageType == error OR messageType == fault)")
	}
	if q.service != "" {
		process := q.service[strings.LastIndex(q.service, ".")+1:]
		parts = append(parts, fmt.Sprintf(`(subsystem == "%s" OR process == "%s")`, q.service, process))
	}
	return strings.Join(parts, " AND ")
}

// unifiedEntry holds the fields of `log show --style ndjson` the agent uses.
type unifiedEntry struct {
	Timestamp   string `json:"timestamp"`
	MessageType string `json:"messageType"`
	Message     string `json:"eventMessage"`
	ProcessPath string `json:"processImagePath"`
	Subsystem   string `json:"subsystem"`
}

// ParseUnifiedLog reads `log show --style ndjson`: one JSON object per
// line, oldest first, then a summary line without a timestamp, which is
// skipped like any other line that isn't an entry.
func ParseUnifiedLog(output []byte) []protocol.LogEntry {
	var entries []protocol.LogEntry
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var raw unifiedEntry
		if json.Unmarshal(scanner.Bytes(), &raw) != nil {
			continue
		}
		at, err := time.Parse(unifiedTime, raw.Timestamp)
		if err != nil {
			continue
		}
		source := raw.Subsystem
		if raw.ProcessPath != "" {
			source = path.Base(raw.ProcessPath)
		}
		entries = append(entries, protocol.LogEntry{Time: at.UnixMilli(), Level: unifiedLevel(raw.MessageType),
			Source: source, Message: raw.Message})
	}
	return entries
}

func unifiedLevel(messageType string) protocol.LogLevel {
	switch messageType {
	case "Fault", "Error":
		return protocol.LogError
	case "Info":
		return protocol.LogInfo
	case "Debug":
		return protocol.LogDebug
	default: // "Default"
		return protocol.LogNotice
	}
}

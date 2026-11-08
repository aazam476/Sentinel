// SPDX-License-Identifier: Apache-2.0

package logs

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// EventLog reads the Windows Event Log through wevtutil, which ships with
// Windows and calls the same Event Log API (wevtapi) a program would.
type EventLog struct {
	runner command.Runner
	clock  Clock
}

// Query reads the System and Application logs, newest first, and merges
// them. A service's own events are usually in Application under its
// name; the Service Control Manager's events about it are in System.
func (e EventLog) Query(ctx context.Context, request protocol.LogQueryPayload) (protocol.LogEntriesPayload, error) {
	q, err := check("windows", request, e.clock.Now())
	if err != nil {
		return protocol.LogEntriesPayload{}, err
	}
	var entries []protocol.LogEntry
	for _, log := range []string{"System", "Application"} {
		output, err := e.runner.Run(ctx, "wevtutil", "qe", log, "/rd:true", "/c:"+strconv.Itoa(q.limit+1),
			"/f:RenderedXml", "/q:"+eventQuery(q))
		if err != nil {
			return protocol.LogEntriesPayload{}, fmt.Errorf("wevtutil: %w", unsupportedIfMissing(err))
		}
		entries = append(entries, ParseEvents(output)...)
	}
	return newestFirst(entries, q.limit), nil
}

// eventQuery is the XPath filter for wevtutil. The service name was
// validated, so it has no quotes to break out with.
func eventQuery(q query) string {
	conditions := []string{fmt.Sprintf("TimeCreated[@SystemTime>='%s']", q.since.UTC().Format("2006-01-02T15:04:05.000Z"))}
	if levels := eventLevels(q.minLevel); levels != "" {
		conditions = append(conditions, levels)
	}
	if q.service != "" {
		conditions = append(conditions, fmt.Sprintf("Provider[@Name='%s']", q.service))
	}
	return "*[System[" + strings.Join(conditions, " and ") + "]]"
}

// eventLevels picks the event levels at minLevel or above: 1 critical,
// 2 error, 3 warning, 4 information, 0 "log always" (shown as info),
// 5 verbose.
func eventLevels(minLevel protocol.LogLevel) string {
	switch minLevel {
	case protocol.LogError:
		return "(Level=1 or Level=2)"
	case protocol.LogWarning:
		return "(Level=1 or Level=2 or Level=3)"
	case protocol.LogNotice, protocol.LogInfo:
		return "(Level=0 or Level=1 or Level=2 or Level=3 or Level=4)"
	default:
		return ""
	}
}

// event holds the parts of a rendered event the agent uses.
type event struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID     int `xml:"EventID"`
		Level       int `xml:"Level"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	Message string `xml:"RenderingInfo>Message"`
}

// ParseEvents reads `wevtutil qe /f:RenderedXml`: <Event> elements one
// after another, with no enclosing element. It stops at the first one it
// can't read, such as one cut off at the end.
func ParseEvents(output []byte) []protocol.LogEntry {
	var entries []protocol.LogEntry
	decoder := xml.NewDecoder(bytes.NewReader(output))
	for {
		var raw event
		if decoder.Decode(&raw) != nil {
			return entries
		}
		at, err := time.Parse(time.RFC3339Nano, raw.System.TimeCreated.SystemTime)
		if err != nil {
			continue
		}
		message := strings.TrimSpace(raw.Message)
		if message == "" {
			message = fmt.Sprintf("Event %d (no message text)", raw.System.EventID)
		}
		entries = append(entries, protocol.LogEntry{Time: at.UnixMilli(), Level: eventLevel(raw.System.Level),
			Source: raw.System.Provider.Name, Message: message})
	}
}

func eventLevel(level int) protocol.LogLevel {
	switch level {
	case 1, 2:
		return protocol.LogError
	case 3:
		return protocol.LogWarning
	case 5:
		return protocol.LogDebug
	default:
		return protocol.LogInfo
	}
}

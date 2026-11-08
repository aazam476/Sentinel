// SPDX-License-Identifier: Apache-2.0

package logs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/services"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Limits on a query.
const (
	defaultLimit  = 200
	maxLimit      = 1000
	defaultWindow = time.Hour
)

// Errors from Reader. They are never reassigned.
var (
	// ErrUnsupported: this machine's log isn't one the agent can read.
	ErrUnsupported = errors.New("this machine's system log isn't supported")
	// ErrInvalidQuery: the query asks for something impossible.
	ErrInvalidQuery = errors.New("invalid log query")
)

// Clock tells the time, for the default start of a query.
type Clock interface {
	Now() time.Time
}

// Reader answers log queries.
type Reader interface {
	Query(ctx context.Context, query protocol.LogQueryPayload) (protocol.LogEntriesPayload, error)
}

// New returns the Reader for an operating system (runtime.GOOS).
func New(goos string, runner command.Runner, clock Clock) Reader {
	switch goos {
	case "linux":
		return Journald{runner: runner, clock: clock}
	case "darwin":
		return UnifiedLog{runner: runner, clock: clock}
	case "windows":
		return EventLog{runner: runner, clock: clock}
	default:
		return unsupported{}
	}
}

// NewJournald returns a Reader for journald.
func NewJournald(runner command.Runner, clock Clock) Journald {
	return Journald{runner: runner, clock: clock}
}

// NewUnifiedLog returns a Reader for macOS's unified log.
func NewUnifiedLog(runner command.Runner, clock Clock) UnifiedLog {
	return UnifiedLog{runner: runner, clock: clock}
}

// NewEventLog returns a Reader for the Windows Event Log.
func NewEventLog(runner command.Runner, clock Clock) EventLog {
	return EventLog{runner: runner, clock: clock}
}

type unsupported struct{}

func (unsupported) Query(context.Context, protocol.LogQueryPayload) (protocol.LogEntriesPayload, error) {
	return protocol.LogEntriesPayload{}, ErrUnsupported
}

// query is a checked LogQueryPayload with its defaults filled in.
type query struct {
	service  string
	since    time.Time
	minLevel protocol.LogLevel
	limit    int
}

// check validates a query for goos and fills in the defaults.
func check(goos string, request protocol.LogQueryPayload, now time.Time) (query, error) {
	checked := query{service: request.Service, minLevel: request.MinLevel, limit: cmp.Or(request.Limit, defaultLimit),
		since: now.Add(-defaultWindow)}
	if request.Since != 0 {
		checked.since = time.UnixMilli(request.Since).UTC()
	}
	if checked.limit < 1 || checked.limit > maxLimit {
		return query{}, fmt.Errorf("%w: the limit must be 1 to %d", ErrInvalidQuery, maxLimit)
	}
	if checked.minLevel != "" && !slices.Contains(levels(), checked.minLevel) {
		return query{}, fmt.Errorf("%w: unknown level %q", ErrInvalidQuery, checked.minLevel)
	}
	if checked.service != "" {
		if err := services.ValidateName(goos, checked.service); err != nil {
			return query{}, fmt.Errorf("%w: %w", ErrInvalidQuery, err)
		}
	}
	return checked, nil
}

// levels lists the levels from most to least severe.
func levels() []protocol.LogLevel {
	return []protocol.LogLevel{protocol.LogError, protocol.LogWarning, protocol.LogNotice, protocol.LogInfo, protocol.LogDebug}
}

// includes reports whether a query for minLevel wants entries at level.
func includes(minLevel, level protocol.LogLevel) bool {
	if minLevel == "" {
		return true
	}
	return slices.Index(levels(), level) <= slices.Index(levels(), minLevel)
}

// newestFirst sorts entries newest first and keeps at most limit.
func newestFirst(entries []protocol.LogEntry, limit int) protocol.LogEntriesPayload {
	slices.SortStableFunc(entries, func(a, b protocol.LogEntry) int { return cmp.Compare(b.Time, a.Time) })
	if entries == nil {
		entries = []protocol.LogEntry{}
	}
	if len(entries) > limit {
		return protocol.LogEntriesPayload{Entries: entries[:limit], Truncated: true}
	}
	return protocol.LogEntriesPayload{Entries: entries}
}

func unsupportedIfMissing(err error) error {
	if errors.Is(err, command.ErrNotFound) {
		return fmt.Errorf("%w: %w", ErrUnsupported, err)
	}
	return err
}

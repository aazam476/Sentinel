// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Database runs transactions (internal/server/store).
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// How long each table keeps its rows by default (§9.1). Admins can change
// them on the Settings page.
const (
	RawRetention    = 24 * time.Hour
	MinuteRetention = 14 * 24 * time.Hour
	HourRetention   = 365 * 24 * time.Hour
	// MaxHourRetention is the longest hourly history the Settings page
	// allows: five years.
	MaxHourRetention = 5 * 365 * 24 * time.Hour
)

// Retention is how long each table keeps its rows.
type Retention struct {
	Raw, Minute, Hour time.Duration
}

// DefaultRetention is §9.1's: a day, two weeks, a year.
func DefaultRetention() Retention {
	return Retention{Raw: RawRetention, Minute: MinuteRetention, Hour: HourRetention}
}

// Service stores and queries metrics.
type Service struct {
	database Database
	clock    Clock

	mutex sync.Mutex
	// agents holds, per agent, its newest sample and the counters needed
	// to turn the next sample's totals into rates.
	agents map[int64]*agentState
	// dirtySince is the earliest sample time ingested since the last
	// rollup, or 0 if there was none. Rollup recomputes from there.
	dirtySince int64
	// retention is how long each table keeps rows (SetRetention).
	retention Retention
}

// agentState is what Service remembers about one agent between samples.
type agentState struct {
	latest   Snapshot
	counters counters
}

// counters are the totals since boot that rates are computed from.
type counters struct {
	time                                  int64
	received, sent, diskRead, diskWritten uint64
}

// NewService returns a Service. Rollups at first cover the last two hours,
// so samples stored just before a restart still get rolled up.
func NewService(database Database, clock Clock) *Service {
	return &Service{
		database:   database,
		clock:      clock,
		agents:     map[int64]*agentState{},
		dirtySince: clock.Now().Add(-2 * time.Hour).UnixMilli(),
		retention:  DefaultRetention(),
	}
}

// SetRetention changes how long each table keeps rows, from the next prune.
func (s *Service) SetRetention(retention Retention) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.retention = retention
}

// currentRetention is the retention in force.
func (s *Service) currentRetention() Retention {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.retention
}

// Snapshot is an agent's newest sample, with rates worked out.
type Snapshot struct {
	protocol.MetricsSample
	// Values holds every metric this sample produced, by name. Rates are
	// missing after the agent's first sample or a reboot.
	Values map[string]float64
}

// Latest returns the newest sample of an agent since the server started.
func (s *Service) Latest(agentID int64) (Snapshot, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	state, ok := s.agents[agentID]
	if !ok {
		return Snapshot{}, false
	}
	return state.latest, true
}

// Forget drops what is remembered about an agent, e.g. after revocation.
func (s *Service) Forget(agentID int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.agents, agentID)
}

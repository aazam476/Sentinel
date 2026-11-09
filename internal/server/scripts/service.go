// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// Database runs transactions (internal/server/store).
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// AuditLog records changes in the same transaction as the change.
type AuditLog interface {
	RecordInTx(ctx context.Context, tx *sql.Tx, entry audit.Entry) error
}

// Stream is a script's output stream (a *hub.Stream).
type Stream interface {
	Frames() <-chan []byte
	Done() <-chan struct{}
	Reason() string
	Close(ctx context.Context, reason string)
}

// Hub runs scripts on agents; HubOf adapts the real hub.
type Hub interface {
	IsOnline(agentID int64) bool
	OpenScript(ctx context.Context, agentID int64, payload func(protocol.StreamID) any) (Stream, error)
}

// HubOf adapts the server's hub to Hub.
func HubOf(h *hub.Hub) Hub { return hubAdapter{h} }

type hubAdapter struct{ hub *hub.Hub }

func (a hubAdapter) IsOnline(agentID int64) bool { return a.hub.IsOnline(agentID) }

func (a hubAdapter) OpenScript(ctx context.Context, agentID int64, payload func(protocol.StreamID) any) (Stream, error) {
	stream, err := a.hub.OpenStream(ctx, agentID, protocol.ActionScriptRun, payload, nil)
	if err != nil {
		return nil, err // the hub's errors say what went wrong already
	}
	return stream, nil
}

// Fleet lists the machines scripts can run on (internal/server/fleet).
type Fleet interface {
	AllAgents(ctx context.Context) ([]fleet.Agent, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Options configures a Service.
type Options struct {
	Database Database
	AuditLog AuditLog
	Hub      Hub
	Fleet    Fleet
	Clock    Clock
	Logger   *slog.Logger
}

// Service keeps scripts and runs them.
type Service struct {
	database Database
	audit    AuditLog
	hub      Hub
	fleet    Fleet
	clock    Clock
	logger   *slog.Logger

	mutex sync.Mutex
	// live holds the runs going now, by run ID.
	live map[int64]*liveRun
	// running tracks the goroutines running scripts, for Wait.
	running sync.WaitGroup
}

// New returns a Service.
func New(options Options) *Service {
	return &Service{
		database: options.Database, audit: options.AuditLog, hub: options.Hub, fleet: options.Fleet,
		clock: options.Clock, logger: options.Logger, live: map[int64]*liveRun{},
	}
}

// Wait waits for every running script to end; the server calls it while
// shutting down, after closing the agents' connections, which ends them.
func (s *Service) Wait() {
	s.running.Wait()
}

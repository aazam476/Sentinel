// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"database/sql"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Database runs transactions (internal/server/store).
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// AuditLog records changes in the same transaction as the change itself.
type AuditLog interface {
	RecordInTx(ctx context.Context, tx *sql.Tx, entry audit.Entry) error
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Options are what NewService needs.
type Options struct {
	Database Database
	AuditLog AuditLog
	Clock    Clock
}

// Service manages agents and enrollment tokens.
type Service struct {
	database Database
	audit    AuditLog
	clock    Clock
}

// NewService returns a Service.
func NewService(options Options) *Service {
	return &Service{database: options.Database, audit: options.AuditLog, clock: options.Clock}
}

func (s *Service) now() int64 {
	return s.clock.Now().UnixMilli()
}

// byUser is an audit entry for something a signed-in user did.
func byUser(actor auth.Session, request auth.RequestInfo, action string, details map[string]any) audit.Entry {
	userID := actor.UserID
	return audit.Entry{
		UserID: &userID, Username: actor.Username, Action: action,
		Outcome: audit.OutcomeSuccess, IP: request.IP, Details: details,
	}
}

// aboutAgent adds the agent an audit entry is about.
func aboutAgent(entry audit.Entry, agent Agent) audit.Entry {
	agentID := agent.ID
	entry.AgentID = &agentID
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["agentId"] = agent.PublicID
	entry.Details["machine"] = agent.Name()
	return entry
}

// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Agent is a managed machine.
type Agent struct {
	// ID is the internal number other tables and the audit log refer to.
	ID int64
	// PublicID is the ULID the agent, the protocol and the API use.
	PublicID        string
	DisplayName     string
	Hostname        string
	OS              string
	Arch            string
	AgentVersion    string
	ProtocolVersion int
	PublicKey       ed25519.PublicKey
	EnrolledAt      int64
	LastSeenAt      *int64
	RevokedAt       *int64
	// MaintenanceUntil is when the machine's maintenance window ends; while
	// it lasts, its alerts notify nobody. Nil when it isn't in maintenance.
	MaintenanceUntil *int64
	Tags             []string
	// SystemInfo is what the agent last reported about its machine; empty
	// until its first report.
	SystemInfo protocol.SystemInfo
}

// Name is what the UI calls the machine: its display name if an admin set
// one, otherwise its hostname.
func (a Agent) Name() string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Hostname
}

// ListAgents returns every machine that hasn't been revoked, by name.
func (s *Service) ListAgents(ctx context.Context, actor auth.Session) ([]Agent, error) {
	if err := auth.Authorize(actor, auth.PermissionViewFleet); err != nil {
		return nil, err
	}
	var agents []Agent
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		agents, err = listAgents(ctx, tx)
		return err
	})
	return agents, err
}

// AllAgents returns every machine that hasn't been revoked, for background
// work that acts for no one, such as scheduled scripts. Callers acting for
// a person use ListAgents, which checks their permission.
func (s *Service) AllAgents(ctx context.Context) ([]Agent, error) {
	var agents []Agent
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		agents, err = listAgents(ctx, tx)
		return err
	})
	return agents, err
}

// GetAgent returns one machine that hasn't been revoked.
func (s *Service) GetAgent(ctx context.Context, actor auth.Session, publicID string) (Agent, error) {
	if err := auth.Authorize(actor, auth.PermissionViewFleet); err != nil {
		return Agent{}, err
	}
	var agent Agent
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		agent, err = findActiveAgent(ctx, tx, publicID)
		return err
	})
	return agent, err
}

// UpdateAgentInput changes a machine. Nil fields are left as they are.
type UpdateAgentInput struct {
	// DisplayName "" goes back to showing the hostname.
	DisplayName *string
	// Tags replaces all of the machine's tags.
	Tags *[]string
}

// UpdateAgent renames or re-tags a machine.
func (s *Service) UpdateAgent(ctx context.Context, actor auth.Session, publicID string, input UpdateAgentInput, request auth.RequestInfo) (Agent, error) {
	if err := auth.Authorize(actor, auth.PermissionManageMachines); err != nil {
		return Agent{}, err
	}
	changes, err := cleanAgentChanges(input)
	if err != nil {
		return Agent{}, err
	}
	var updated Agent
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		agent, err := findActiveAgent(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if err := applyAgentChanges(ctx, tx, agent.ID, changes); err != nil {
			return err
		}
		if updated, err = findActiveAgent(ctx, tx, publicID); err != nil {
			return err
		}
		return s.audit.RecordInTx(ctx, tx, aboutAgent(byUser(actor, request, "agent.update", changes.details()), updated))
	})
	return updated, err
}

// agentChanges is an UpdateAgentInput after validation.
type agentChanges struct {
	displayName *string
	tags        *[]string
}

func cleanAgentChanges(input UpdateAgentInput) (agentChanges, error) {
	var changes agentChanges
	if input.DisplayName != nil {
		name, err := cleanText("displayName", *input.DisplayName, maxDisplayNameLength)
		if err != nil {
			return agentChanges{}, err
		}
		changes.displayName = &name
	}
	if input.Tags != nil {
		tags, err := cleanTags(*input.Tags)
		if err != nil {
			return agentChanges{}, err
		}
		changes.tags = &tags
	}
	return changes, nil
}

func (c agentChanges) details() map[string]any {
	details := map[string]any{}
	if c.displayName != nil {
		details["displayName"] = *c.displayName
	}
	if c.tags != nil {
		details["tags"] = *c.tags
	}
	return details
}

func applyAgentChanges(ctx context.Context, tx *sql.Tx, agentID int64, changes agentChanges) error {
	if changes.displayName != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET display_name = ? WHERE id = ?`, *changes.displayName, agentID); err != nil {
			return fmt.Errorf("rename agent: %w", err)
		}
	}
	if changes.tags != nil {
		return replaceTags(ctx, tx, agentID, *changes.tags)
	}
	return nil
}

// MaxMaintenance is the longest a maintenance window can be set for.
const MaxMaintenance = 30 * 24 * time.Hour

// SetMaintenance puts a machine in maintenance until a time (UTC Unix
// milliseconds), or takes it out with nil, and audits it.
func (s *Service) SetMaintenance(ctx context.Context, actor auth.Session, publicID string, until *int64, request auth.RequestInfo) (Agent, error) {
	if err := auth.Authorize(actor, auth.PermissionManageMachines); err != nil {
		return Agent{}, err
	}
	now := s.now()
	if until != nil && (*until <= now || *until > now+MaxMaintenance.Milliseconds()) {
		return Agent{}, &auth.ValidationError{Field: "until", Message: "Choose a time in the next 30 days."}
	}
	var updated Agent
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		agent, err := findActiveAgent(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET maintenance_until = ? WHERE id = ?`, until, agent.ID); err != nil {
			return fmt.Errorf("set maintenance: %w", err)
		}
		agent.MaintenanceUntil = until
		updated = agent
		action, details := "agent.maintenance.end", map[string]any(nil)
		if until != nil {
			action, details = "agent.maintenance.start", map[string]any{"until": *until}
		}
		return s.audit.RecordInTx(ctx, tx, aboutAgent(byUser(actor, request, action, details), agent))
	})
	return updated, err
}

// RevokeAgent stops a machine from ever connecting again. The
// caller must also close its live connection, if it has one: see
// hub.Hub.Disconnect.
func (s *Service) RevokeAgent(ctx context.Context, actor auth.Session, publicID string, request auth.RequestInfo) (Agent, error) {
	if err := auth.Authorize(actor, auth.PermissionRevokeAgents); err != nil {
		return Agent{}, err
	}
	var revoked Agent
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		agent, err := findActiveAgent(ctx, tx, publicID)
		if err != nil {
			return err
		}
		now := s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET revoked_at = ? WHERE id = ?`, now, agent.ID); err != nil {
			return fmt.Errorf("revoke agent: %w", err)
		}
		agent.RevokedAt = &now
		revoked = agent
		return s.audit.RecordInTx(ctx, tx, aboutAgent(byUser(actor, request, "agent.revoke", nil), agent))
	})
	return revoked, err
}

// errAgentNotFound means no agent matched. It is never reassigned.
var errAgentNotFound = errors.New("agent not found")

const selectAgent = `SELECT id, public_id, display_name, hostname, os, arch, agent_version, protocol_version,
	public_key, enrolled_at, last_seen_at, revoked_at, maintenance_until, system_info_json,
	(SELECT json_group_array(tag) FROM (SELECT tag FROM agent_tags WHERE agent_id = agents.id ORDER BY tag)),
	(SELECT json_group_array(json_object('name', capability, 'enabled', json(iif(enabled, 'true', 'false')), 'reason', reason))
		FROM (SELECT * FROM agent_capabilities WHERE agent_id = agents.id ORDER BY capability))
	FROM agents`

// findActiveAgent returns the agent with publicID, or auth.ErrNotFound if
// there is none or it was revoked.
func findActiveAgent(ctx context.Context, tx *sql.Tx, publicID string) (Agent, error) {
	agent, err := findAgent(ctx, tx, "public_id = ?", publicID)
	if errors.Is(err, errAgentNotFound) || (err == nil && agent.RevokedAt != nil) {
		return Agent{}, auth.ErrNotFound
	}
	return agent, err
}

// findAgent returns the agent matching condition, a SQL condition with one
// parameter, always written in this package.
func findAgent(ctx context.Context, tx *sql.Tx, condition string, value any) (Agent, error) {
	agent, err := scanAgent(tx.QueryRowContext(ctx, selectAgent+" WHERE "+condition, value))
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, errAgentNotFound
	}
	return agent, err
}

func listAgents(ctx context.Context, tx *sql.Tx) ([]Agent, error) {
	rows, err := tx.QueryContext(ctx, selectAgent+
		` WHERE revoked_at IS NULL ORDER BY lower(CASE display_name WHEN '' THEN hostname ELSE display_name END), id`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only; nothing to report

	agents := []Agent{}
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func scanAgent(row interface{ Scan(...any) error }) (Agent, error) {
	var agent Agent
	var systemInfo, tags, capabilities string
	var publicKey []byte
	err := row.Scan(&agent.ID, &agent.PublicID, &agent.DisplayName, &agent.Hostname, &agent.OS, &agent.Arch,
		&agent.AgentVersion, &agent.ProtocolVersion, &publicKey, &agent.EnrolledAt, &agent.LastSeenAt,
		&agent.RevokedAt, &agent.MaintenanceUntil, &systemInfo, &tags, &capabilities)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, err
	}
	if err != nil {
		return Agent{}, fmt.Errorf("read agent: %w", err)
	}
	agent.PublicKey = publicKey
	if err := json.Unmarshal([]byte(systemInfo), &agent.SystemInfo); err != nil {
		return Agent{}, fmt.Errorf("read agent %s system info: %w", agent.PublicID, err)
	}
	if err := json.Unmarshal([]byte(tags), &agent.Tags); err != nil {
		return Agent{}, fmt.Errorf("read agent %s tags: %w", agent.PublicID, err)
	}
	// Capabilities live in their own table, so they can be queried.
	if err := json.Unmarshal([]byte(capabilities), &agent.SystemInfo.Capabilities); err != nil {
		return Agent{}, fmt.Errorf("read agent %s capabilities: %w", agent.PublicID, err)
	}
	return agent, nil
}

func replaceTags(ctx context.Context, tx *sql.Tx, agentID int64, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_tags WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("clear agent tags: %w", err)
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_tags (agent_id, tag) VALUES (?, ?)`, agentID, tag); err != nil {
			return fmt.Errorf("add agent tag: %w", err)
		}
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0

package updates

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// Update states.
const (
	StateUpdating   = "updating"
	StateUpdated    = "updated"
	StateFailed     = "failed"
	StateRolledBack = "rolled_back"
)

// Target says which machines to update: kind "all", "tag" (Value is a tag)
// or "agent" (a machine's ID).
type Target struct {
	Kind, Value string
}

// Outcome says what an update request did.
type Outcome struct {
	// Started are the machines told to update.
	Started []string
	// Skipped are the machines left alone, each with why.
	Skipped map[string]string
}

// Status is the last update a machine was asked to make.
type Status struct {
	Version    string
	State      string
	Error      string
	StartedAt  int64
	FinishedAt *int64
}

// Update tells the target machines to update to version (the newest hosted
// build if ""), and audits it.
func (s *Service) Update(ctx context.Context, actor auth.Session, request auth.RequestInfo, target Target, version string) (Outcome, error) {
	if err := auth.Authorize(actor, auth.PermissionManageMachines); err != nil {
		return Outcome{}, err
	}
	agents, err := s.targets(ctx, target)
	if err != nil {
		return Outcome{}, err
	}
	outcome := s.updateAgents(ctx, agents, version)
	if len(outcome.Started) == 0 && target.Kind == "agent" {
		for _, reason := range outcome.Skipped {
			return outcome, &InvalidError{"target", reason}
		}
	}
	err = s.audit(ctx, userEntry(actor, request, "agent.update.start", map[string]any{
		"target": target.Kind + " " + target.Value, "version": version, "started": len(outcome.Started), "skipped": len(outcome.Skipped),
	}))
	return outcome, err
}

func (s *Service) targets(ctx context.Context, target Target) ([]fleet.Agent, error) {
	agents, err := s.options.Fleet.AllAgents(ctx)
	if err != nil {
		return nil, err
	}
	switch target.Kind {
	case "all":
		return agents, nil
	case "tag":
		return slices.DeleteFunc(agents, func(agent fleet.Agent) bool {
			return !slices.ContainsFunc(agent.Tags, func(tag string) bool { return strings.EqualFold(tag, target.Value) })
		}), nil
	case "agent":
		agents = slices.DeleteFunc(agents, func(agent fleet.Agent) bool { return agent.PublicID != target.Value })
		if len(agents) == 0 {
			return nil, ErrNotFound
		}
		return agents, nil
	}
	return nil, &InvalidError{"target", "choose all machines, a tag or one machine"}
}

// updateAgents asks each machine that is online, has a build and doesn't
// already run it, to update.
func (s *Service) updateAgents(ctx context.Context, agents []fleet.Agent, version string) Outcome {
	outcome := Outcome{Started: []string{}, Skipped: map[string]string{}}
	for _, agent := range agents {
		if reason := s.updateAgent(ctx, agent, version); reason != "" {
			outcome.Skipped[agent.PublicID] = reason
			continue
		}
		outcome.Started = append(outcome.Started, agent.PublicID)
	}
	return outcome
}

// updateAgent starts one machine's update, or says why it didn't.
func (s *Service) updateAgent(ctx context.Context, agent fleet.Agent, version string) string {
	if !s.options.Hub.IsOnline(agent.ID) {
		return "it is offline"
	}
	build, err := s.buildFor(agent.OS, agent.Arch, version)
	if err != nil {
		return fmt.Sprintf("there is no build for %s/%s", agent.OS, agent.Arch)
	}
	if build.Version == agent.AgentVersion {
		return "it already runs " + build.Version
	}
	payload := protocol.AgentUpdatePayload{Version: build.Version, Path: build.Path(), Signature: string(build.signature)}
	if err := s.options.Hub.Request(ctx, agent.ID, protocol.ActionAgentUpdate, payload, nil); err != nil {
		var refusal *protocol.Error
		if errors.As(err, &refusal) {
			return refusal.Message
		}
		return "it couldn't be asked: " + err.Error()
	}
	now := s.options.Clock.Now().UnixMilli()
	err = s.options.Database.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO agent_updates (agent_id, version, state, error, started_at, finished_at)
			VALUES (?, ?, 'updating', '', ?, NULL) ON CONFLICT (agent_id) DO UPDATE SET version = excluded.version, state = 'updating',
			error = '', started_at = excluded.started_at, finished_at = NULL`, agent.ID, build.Version, now)
		return err
	})
	if err != nil {
		s.options.Logger.Error("could not record an update", "agent", agent.PublicID, "error", err)
	}
	return ""
}

// RecordResult stores and audits what a machine reported about its update.
func (s *Service) RecordResult(ctx context.Context, agentID int64, result protocol.AgentUpdateResultPayload) error {
	state := StateUpdated
	switch {
	case result.RolledBack:
		state = StateRolledBack
	case !result.OK:
		state = StateFailed
	}
	now := s.options.Clock.Now().UnixMilli()
	return s.options.Database.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO agent_updates (agent_id, version, state, error, started_at, finished_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (agent_id) DO UPDATE SET version = excluded.version, state = excluded.state,
			error = excluded.error, finished_at = excluded.finished_at`, agentID, result.Version, state, result.Error, now, now)
		if err != nil {
			return fmt.Errorf("record the update: %w", err)
		}
		outcome := audit.OutcomeSuccess
		if state != StateUpdated {
			outcome = audit.OutcomeFailure
		}
		return s.options.AuditLog.RecordInTx(ctx, tx, audit.Entry{
			AgentID: &agentID, Action: "agent.update.result", Outcome: outcome,
			Details: map[string]any{"version": result.Version, "state": state, "error": result.Error},
		})
	})
}

// Statuses returns each machine's last update, by internal ID.
func (s *Service) Statuses(ctx context.Context) (map[int64]Status, error) {
	statuses := map[int64]Status{}
	err := s.options.Database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT agent_id, version, state, error, started_at, finished_at FROM agent_updates`)
		if err != nil {
			return fmt.Errorf("read updates: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			var status Status
			if err := rows.Scan(&id, &status.Version, &status.State, &status.Error, &status.StartedAt, &status.FinishedAt); err != nil {
				return fmt.Errorf("read an update: %w", err)
			}
			statuses[id] = status
		}
		return rows.Err()
	})
	return statuses, err
}

// autoKey is the setting that turns automatic updates on.
const autoKey = "updates.auto"

// AutoUpdate says whether machines update by themselves.
func (s *Service) AutoUpdate(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.options.Database.Read(ctx, func(tx *sql.Tx) error {
		var value string
		err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, autoKey).Scan(&value)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the auto-update setting: %w", err)
		}
		return json.Unmarshal([]byte(value), &enabled)
	})
	return enabled, err
}

// SetAutoUpdate turns automatic updates on or off, and audits it.
func (s *Service) SetAutoUpdate(ctx context.Context, actor auth.Session, request auth.RequestInfo, enabled bool) error {
	if err := auth.Authorize(actor, auth.PermissionManageMachines); err != nil {
		return err
	}
	return s.options.Database.Write(ctx, func(tx *sql.Tx) error {
		value, _ := json.Marshal(enabled) // a bool always encodes
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json`, autoKey, string(value)); err != nil {
			return fmt.Errorf("save the auto-update setting: %w", err)
		}
		return s.options.AuditLog.RecordInTx(ctx, tx, userEntry(actor, request, "agent.update.auto", map[string]any{"enabled": enabled}))
	})
}

// RunAuto updates every online machine to the newest build, if automatic
// updates are on. The scheduler calls it every hour. A machine whose last
// update to that version failed isn't asked again.
func (s *Service) RunAuto(ctx context.Context) error {
	enabled, err := s.AutoUpdate(ctx)
	if err != nil || !enabled {
		return err
	}
	agents, err := s.options.Fleet.AllAgents(ctx)
	if err != nil {
		return err
	}
	statuses, err := s.Statuses(ctx)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		build, err := s.buildFor(agent.OS, agent.Arch, "")
		if err != nil {
			continue
		}
		if last, found := statuses[agent.ID]; found && last.Version == build.Version && last.State != StateUpdated {
			continue // tried already; an admin can retry by hand
		}
		if reason := s.updateAgent(ctx, agent, build.Version); reason == "" {
			s.options.Logger.Info("updating a machine automatically", "agent", agent.PublicID, "to", build.Version)
		}
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aazam476/sentinel/internal/protocol"
)

// The methods in this file are for internal/server/hub, which calls them as
// agents connect and report in. They take no user session: the agent has
// proven who it is with its key instead.

// AgentForHandshake returns the agent with publicID so the hub can check its
// signature. It returns ErrUnknownAgent or ErrAgentRevoked when the agent
// may not connect.
func (s *Service) AgentForHandshake(ctx context.Context, publicID string) (Agent, error) {
	var agent Agent
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		agent, err = findAgent(ctx, tx, "public_id = ?", publicID)
		return err
	})
	switch {
	case errors.Is(err, errAgentNotFound):
		return Agent{}, ErrUnknownAgent
	case err != nil:
		return Agent{}, err
	case agent.RevokedAt != nil:
		return Agent{}, ErrAgentRevoked
	}
	return agent, nil
}

// IsRevoked reports whether the agent has been revoked.
func (s *Service) IsRevoked(ctx context.Context, agentID int64) (bool, error) {
	var revoked bool
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT revoked_at IS NOT NULL FROM agents WHERE id = ?`, agentID).Scan(&revoked)
		if err != nil {
			return fmt.Errorf("check agent revocation: %w", err)
		}
		return nil
	})
	return revoked, err
}

// RecordConnected notes that the agent finished a handshake, and which
// versions it runs now (they change when it updates).
func (s *Service) RecordConnected(ctx context.Context, agentID int64, agentVersion string, protocolVersion int) error {
	version, err := requiredText("agentVersion", agentVersion, maxReportedLength)
	if err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE agents SET agent_version = ?, protocol_version = ?, last_seen_at = ? WHERE id = ?`,
			version, protocolVersion, s.now(), agentID)
		if err != nil {
			return fmt.Errorf("record agent connection: %w", err)
		}
		return nil
	})
}

// RecordSeen notes that the agent was connected until now. The hub calls it
// when a connection ends; while connected, the agent counts as seen now.
func (s *Service) RecordSeen(ctx context.Context, agentID int64) error {
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET last_seen_at = ? WHERE id = ?`, s.now(), agentID); err != nil {
			return fmt.Errorf("record agent last seen: %w", err)
		}
		return nil
	})
}

// SaveSystemInfo stores what the agent reported about its machine,
// including which capabilities its local policy file allows.
func (s *Service) SaveSystemInfo(ctx context.Context, agentID int64, info protocol.SystemInfo) error {
	info, err := cleanSystemInfo(info)
	if err != nil {
		return err
	}
	capabilities := info.Capabilities
	info.Capabilities = nil // stored in their own table
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("encode system info: %w", err)
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE agents SET hostname = ?, os = ?, arch = ?, system_info_json = ? WHERE id = ?`,
			info.Hostname, info.OS, info.Arch, string(infoJSON), agentID)
		if err != nil {
			return fmt.Errorf("save system info: %w", err)
		}
		return replaceCapabilities(ctx, tx, agentID, capabilities)
	})
}

// maxCapabilities is far more than the features Sentinel has; it only stops
// a broken agent from filling the table.
const maxCapabilities = 64

// cleanSystemInfo applies the usual text checks to everything the UI shows.
func cleanSystemInfo(info protocol.SystemInfo) (protocol.SystemInfo, error) {
	var err error
	if info.Hostname, err = requiredText("hostname", info.Hostname, maxHostnameLength); err != nil {
		return protocol.SystemInfo{}, err
	}
	fields := []struct {
		name  string
		value *string
	}{
		{"os", &info.OS}, {"platform", &info.Platform}, {"platformVersion", &info.PlatformVersion},
		{"kernelVersion", &info.KernelVersion}, {"arch", &info.Arch}, {"cpuModel", &info.CPUModel},
		{"agentVersion", &info.AgentVersion},
	}
	for _, field := range fields {
		if *field.value, err = cleanText(field.name, *field.value, maxReportedLength); err != nil {
			return protocol.SystemInfo{}, err
		}
	}
	if len(info.Capabilities) > maxCapabilities {
		return protocol.SystemInfo{}, fmt.Errorf("agent reported %d capabilities, more than %d", len(info.Capabilities), maxCapabilities)
	}
	for i, capability := range info.Capabilities {
		if info.Capabilities[i].Name, err = requiredText("capability", capability.Name, maxReportedLength); err != nil {
			return protocol.SystemInfo{}, err
		}
		if info.Capabilities[i].Reason, err = cleanText("reason", capability.Reason, maxReportedLength); err != nil {
			return protocol.SystemInfo{}, err
		}
	}
	return info, nil
}

func replaceCapabilities(ctx context.Context, tx *sql.Tx, agentID int64, capabilities []protocol.Capability) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_capabilities WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("clear capabilities: %w", err)
	}
	for _, capability := range capabilities {
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO agent_capabilities (agent_id, capability, enabled, reason) VALUES (?, ?, ?, ?)`,
			agentID, capability.Name, capability.Enabled, capability.Reason)
		if err != nil {
			return fmt.Errorf("save capability: %w", err)
		}
	}
	return nil
}

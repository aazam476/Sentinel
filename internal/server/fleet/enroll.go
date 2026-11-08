// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// EnrollInput is what a new agent sends, plus where it connected from.
type EnrollInput struct {
	Token           string
	PublicKey       []byte
	Hostname        string
	OS              string
	Arch            string
	AgentVersion    string
	ProtocolVersion int
	IP              string
}

// Enroll adds a new agent if its enrollment token is valid, using up one
// of the token's uses. Every attempt is audited; a refused one returns
// ErrEnrollmentRefused whatever the reason, and the reason goes only into
// the audit log.
func (s *Service) Enroll(ctx context.Context, input EnrollInput) (Agent, error) {
	row, err := newAgentRow(input)
	if err != nil {
		return Agent{}, err
	}
	var enrolled Agent
	var refusal string
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		tokenID, reason, err := s.useToken(ctx, tx, input.Token)
		if err != nil {
			return err
		}
		if reason != "" {
			refusal = reason
			return s.audit.RecordInTx(ctx, tx, enrollAudit(input, audit.OutcomeFailure, map[string]any{"reason": reason}))
		}
		enrolled, err = s.insertAgent(ctx, tx, row, tokenID)
		if err != nil {
			return err
		}
		return s.audit.RecordInTx(ctx, tx, aboutAgent(enrollAudit(input, audit.OutcomeSuccess, map[string]any{"tokenId": tokenID}), enrolled))
	})
	if err == nil && refusal != "" {
		err = ErrEnrollmentRefused
	}
	return enrolled, err
}

// useToken spends one use of the token and returns its ID, or explains why
// it can't be used.
func (s *Service) useToken(ctx context.Context, tx *sql.Tx, token string) (tokenID int64, refusal string, err error) {
	found, err := findToken(ctx, tx, "token_hash = ?", auth.HashToken(token))
	switch {
	case errors.Is(err, errTokenNotFound):
		return 0, "unknown token", nil
	case err != nil:
		return 0, "", err
	case found.RevokedAt != nil:
		return 0, "token revoked", nil
	case s.now() >= found.ExpiresAt:
		return 0, "token expired", nil
	case found.UseCount >= found.MaxUses:
		return 0, "token used up", nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE enrollment_tokens SET use_count = use_count + 1 WHERE id = ?`, found.ID); err != nil {
		return 0, "", fmt.Errorf("use enrollment token: %w", err)
	}
	return found.ID, "", nil
}

// newAgentRow checks what the agent sent. Hostnames and versions end up in
// the UI and the audit log, so they get the same checks as admin input.
func newAgentRow(input EnrollInput) (Agent, error) {
	if len(input.PublicKey) != ed25519.PublicKeySize {
		return Agent{}, &auth.ValidationError{Field: "publicKey", Message: "This isn't an Ed25519 public key."}
	}
	agent := Agent{PublicKey: input.PublicKey, ProtocolVersion: input.ProtocolVersion}
	var err error
	if agent.Hostname, err = requiredText("hostname", input.Hostname, maxHostnameLength); err != nil {
		return Agent{}, err
	}
	if agent.OS, err = requiredText("os", input.OS, maxReportedLength); err != nil {
		return Agent{}, err
	}
	if agent.Arch, err = requiredText("arch", input.Arch, maxReportedLength); err != nil {
		return Agent{}, err
	}
	if agent.AgentVersion, err = requiredText("agentVersion", input.AgentVersion, maxReportedLength); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (s *Service) insertAgent(ctx context.Context, tx *sql.Tx, row Agent, tokenID int64) (Agent, error) {
	now := s.clock.Now()
	publicID := protocol.NewID(now)
	_, err := tx.ExecContext(ctx, `INSERT INTO agents (public_id, hostname, os, arch, agent_version, protocol_version,
		public_key, enrollment_token_id, enrolled_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		publicID, row.Hostname, row.OS, row.Arch, row.AgentVersion, row.ProtocolVersion,
		[]byte(row.PublicKey), tokenID, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return Agent{}, fmt.Errorf("insert agent: %w", err)
	}
	return findAgent(ctx, tx, "public_id = ?", publicID)
}

func enrollAudit(input EnrollInput, outcome audit.Outcome, details map[string]any) audit.Entry {
	hostname, _ := cleanText("hostname", input.Hostname, maxHostnameLength) // already checked; "" if not
	details["hostname"] = hostname
	return audit.Entry{Action: "agent.enroll", Outcome: outcome, IP: input.IP, Details: details}
}

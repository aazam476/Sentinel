// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
)

// ImageUpdate is the last update check of one image tag on one machine.
type ImageUpdate struct {
	Reference       string
	RegistryDigest  string
	UpdateAvailable bool
	// Error says why the check failed (rate limited, unknown registry,
	// login refused), or is empty.
	Error     string
	CheckedAt int64
}

// Updates returns the last checks of a machine's images, by reference.
func (s *Service) Updates(ctx context.Context, agentID int64) (map[string]ImageUpdate, error) {
	updates := map[string]ImageUpdate{}
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT reference, registry_digest, update_available, error, checked_at
			FROM image_updates WHERE agent_id = ?`, agentID)
		if err != nil {
			return fmt.Errorf("read image update checks: %w", err)
		}
		defer rows.Close() //nolint:errcheck // read-only
		for rows.Next() {
			var update ImageUpdate
			if err := rows.Scan(&update.Reference, &update.RegistryDigest, &update.UpdateAvailable, &update.Error, &update.CheckedAt); err != nil {
				return fmt.Errorf("read an image update check: %w", err)
			}
			updates[update.Reference] = update
		}
		return rows.Err()
	})
	return updates, err
}

func (s *Service) record(ctx context.Context, agentID int64, update ImageUpdate) error {
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO image_updates
			(agent_id, reference, registry_digest, update_available, error, checked_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (agent_id, reference) DO UPDATE SET registry_digest = excluded.registry_digest,
			update_available = excluded.update_available, error = excluded.error, checked_at = excluded.checked_at`,
			agentID, update.Reference, update.RegistryDigest, update.UpdateAvailable, update.Error, update.CheckedAt)
		if err != nil {
			return fmt.Errorf("store an image update check: %w", err)
		}
		return nil
	})
}

// Requester sends a request to an agent (internal/server/hub).
type Requester interface {
	Request(ctx context.Context, agentID int64, action string, payload, result any) error
}

// CheckMachine asks a machine's registry, for each tagged image it has,
// whether the tag now points to a newer image, and records the answers. A
// failed check is recorded with its error; it doesn't stop the others.
func (s *Service) CheckMachine(ctx context.Context, agents Requester, agentID int64) ([]ImageUpdate, error) {
	var images protocol.ImageListPayload
	if err := agents.Request(ctx, agentID, protocol.ActionDockerImageList, nil, &images); err != nil {
		return nil, fmt.Errorf("list the machine's images: %w", err)
	}
	var checked []ImageUpdate
	for _, image := range images.Images {
		for _, tag := range image.Tags {
			update := s.checkTag(ctx, agents, agentID, tag)
			if err := s.record(ctx, agentID, update); err != nil {
				return checked, err
			}
			checked = append(checked, update)
		}
	}
	return checked, nil
}

func (s *Service) checkTag(ctx context.Context, agents Requester, agentID int64, tag string) ImageUpdate {
	update := ImageUpdate{Reference: tag, CheckedAt: s.clock.Now().UnixMilli()}
	login, err := s.AuthFor(ctx, tag)
	if err != nil {
		update.Error = err.Error()
		return update
	}
	var result protocol.ImageCheckPayload
	if err := agents.Request(ctx, agentID, protocol.ActionDockerImageCheck,
		protocol.ImageRefPayload{Reference: tag, Auth: login}, &result); err != nil {
		update.Error = strings.TrimSpace(err.Error())
		return update
	}
	update.RegistryDigest, update.UpdateAvailable = result.RegistryDigest, result.UpdateAvailable
	return update
}

// SPDX-License-Identifier: Apache-2.0

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/storage"
	"github.com/aazam476/sentinel/internal/protocol"
)

// storageInterval is how often the agent checks its storage's health
// (§9.5 refreshes every 60 seconds).
const storageInterval = 60 * time.Second

// storageHandlers answers the storage actions (§9.5). The local policy
// can switch storage off (SEC-22).
func (c *connection) storageHandlers() map[string]handler {
	return map[string]handler{
		protocol.ActionStorageOverview: c.storageOverview,
		protocol.ActionZFSPoolStatus:   c.poolStatus,
	}
}

func (c *connection) storageOverview(ctx context.Context, _ protocol.Message) (any, *protocol.Error) {
	if refusal := c.refuseUnlessAllowed(policy.Storage, "The storage view"); refusal != nil {
		return nil, refusal
	}
	overview, err := c.agent.options.Storage.Overview(ctx)
	if err != nil {
		return nil, c.storageError(err, "read the storage")
	}
	return overview, nil
}

func (c *connection) poolStatus(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	if refusal := c.refuseUnlessAllowed(policy.Storage, "The storage view"); refusal != nil {
		return nil, refusal
	}
	var payload protocol.ZFSPoolStatusPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	pool, err := c.agent.options.Storage.PoolStatus(ctx, payload.Pool)
	if err != nil {
		return nil, c.storageError(err, "read the pool "+payload.Pool)
	}
	return pool, nil
}

func (c *connection) storageError(err error, doing string) *protocol.Error {
	switch {
	case errors.Is(err, storage.ErrInvalidPool):
		return &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: err.Error()}
	case errors.Is(err, storage.ErrNoSuchPool):
		return &protocol.Error{Code: protocol.ErrorNotFound, Message: err.Error()}
	}
	c.agent.options.Logger.Warn("a storage request failed", "doing", doing, "error", err)
	return &protocol.Error{Code: protocol.ErrorInternal, Message: "The agent couldn't " + doing + ": " + err.Error()}
}

// watchStorage sends the storage's health when connecting and whenever it
// changes, checking every storageInterval, so the server can raise and
// resolve storage alerts.
func (c *connection) watchStorage(ctx context.Context) {
	if !c.agent.options.Policy.Allows(policy.Storage) {
		return
	}
	timer := c.agent.options.Clock.NewTimer(storageInterval)
	defer timer.Stop()
	var sent []byte
	for {
		sent = c.sendStorageHealth(ctx, sent)
		c.agent.observe(EventStorageChecked)
		select {
		case <-ctx.Done():
			return
		case <-timer.C():
		}
		timer.Reset(storageInterval)
	}
}

// sendStorageHealth sends the current health unless it is what was sent
// last, and returns what has now been sent.
func (c *connection) sendStorageHealth(ctx context.Context, last []byte) []byte {
	overview, err := c.agent.options.Storage.Overview(ctx)
	if err != nil {
		c.agent.options.Logger.Debug("could not check the storage's health", "error", err)
		return last
	}
	health := storage.Health(overview)
	encoded, err := json.Marshal(health)
	if err != nil || bytes.Equal(encoded, last) {
		return last
	}
	event := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(c.agent.options.Clock.Now()), Action: protocol.ActionStorageHealth}
	if err := c.send(ctx, event, health); err != nil {
		return last
	}
	return encoded
}

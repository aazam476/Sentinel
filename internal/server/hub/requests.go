// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"errors"
	"fmt"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// Errors from Request. They are never reassigned.
var (
	// ErrOffline: the agent isn't connected.
	ErrOffline = errors.New("the machine is offline")
	// ErrTimeout: the agent didn't answer within RequestTimeout.
	ErrTimeout = errors.New("the machine didn't answer in time")
	// ErrDisconnected: the connection ended while waiting for the answer.
	ErrDisconnected = errors.New("the machine disconnected before answering")
)

// Request sends an action to an agent and decodes its response's payload
// into result (unless result is nil). A failed response is returned as a
// *protocol.Error.
func (h *Hub) Request(ctx context.Context, agentID int64, action string, payload, result any) error {
	conn, ok := h.connectionOf(agentID)
	if !ok {
		return ErrOffline
	}
	return h.request(ctx, conn, action, payload, result)
}

func (h *Hub) request(ctx context.Context, conn *connection, action string, payload, result any) error {
	ctx, cancel := clock.WithTimeout(ctx, h.clock, RequestTimeout)
	defer cancel()

	message := protocol.Message{Type: protocol.TypeRequest, ID: protocol.NewID(h.clock.Now()), Action: action}
	responses, err := conn.expect(message.ID)
	if err != nil {
		return err
	}
	defer conn.forget(message.ID)
	if err := conn.send(ctx, message, payload); err != nil {
		return err
	}

	select {
	case response, open := <-responses:
		if !open {
			return ErrDisconnected
		}
		return decodeResponse(response, result)
	case <-ctx.Done():
		if errors.Is(context.Cause(ctx), clock.ErrTimeout) {
			return ErrTimeout
		}
		return fmt.Errorf("wait for %s response: %w", action, ctx.Err())
	}
}

func decodeResponse(response protocol.Message, result any) error {
	if response.Error != nil {
		return response.Error
	}
	if result == nil {
		return nil
	}
	return protocol.DecodePayload(response, result)
}

// expect registers a request ID so its response can be delivered.
func (c *connection) expect(id string) (<-chan protocol.Message, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.closed {
		return nil, ErrDisconnected
	}
	responses := make(chan protocol.Message, 1)
	c.pending[id] = responses
	return responses, nil
}

func (c *connection) forget(id string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.pending, id)
}

// deliver hands a response to the request waiting for it. A response no
// one waits for (it came after the timeout) is dropped.
func (c *connection) deliver(response protocol.Message) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if responses, ok := c.pending[response.ID]; ok {
		responses <- response
		delete(c.pending, response.ID)
	}
}

// failPending wakes every waiting request with ErrDisconnected.
func (c *connection) failPending() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.closed = true
	for id, responses := range c.pending {
		close(responses)
		delete(c.pending, id)
	}
}

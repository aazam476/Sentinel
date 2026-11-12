// SPDX-License-Identifier: Apache-2.0

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
)

// dockerHealthInterval is how often the agent looks for containers that
// stopped unexpectedly or keep restarting (§9.9).
const dockerHealthInterval = 60 * time.Second

// watchContainers tells the server about container problems whenever they
// change, while the connection lasts. A machine without Docker sends
// nothing.
func (c *connection) watchContainers(ctx context.Context) {
	if !c.agent.options.Policy.Allows(policy.Docker) {
		return
	}
	timer := c.agent.options.Clock.NewTimer(dockerHealthInterval)
	defer timer.Stop()
	var sent []byte
	for {
		sent = c.sendDockerHealth(ctx, sent)
		c.agent.observe(EventDockerChecked)
		select {
		case <-ctx.Done():
			return
		case <-timer.C():
		}
		timer.Reset(dockerHealthInterval)
	}
}

// sendDockerHealth sends the current problems unless they are what was
// sent last, and returns what has now been sent.
func (c *connection) sendDockerHealth(ctx context.Context, last []byte) []byte {
	problems, err := c.agent.options.Docker.Problems(ctx)
	if err != nil {
		return last // no Docker here, or not now: nothing to report
	}
	health := protocol.DockerHealthPayload{Problems: problems}
	encoded, err := json.Marshal(health)
	if err != nil || bytes.Equal(encoded, last) {
		return last
	}
	event := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(c.agent.options.Clock.Now()), Action: protocol.ActionDockerHealth}
	if err := c.send(ctx, event, health); err != nil {
		return last
	}
	return encoded
}

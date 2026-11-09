// SPDX-License-Identifier: Apache-2.0

package protocol

// ActionAgentUpdate asks the agent to update itself (§11.3). It answers at
// once; downloading, checking and trying the new build take a while, and
// the outcome comes later as an ActionAgentUpdateResult event (or, when it
// worked, as the new version in the agent's next hello).
const ActionAgentUpdate = "agent.update"

// ActionAgentUpdateResult is the event an agent sends about an update it
// tried: once the new version has connected, or when it was refused or
// rolled back (SEC-31, SEC-32).
const ActionAgentUpdateResult = "agent.update_result"

// AgentUpdatePayload says where the new build is and what it must be.
type AgentUpdatePayload struct {
	Version string `json:"version"`
	// Path is where the server serves the build, e.g.
	// "/install/updates/v1.2.0/sentinel-agent_v1.2.0_linux_amd64". The
	// agent fetches it from its own server only.
	Path string `json:"path"`
	// Signature is the build's release signature file (SEC-30).
	Signature string `json:"signature"`
}

// AgentUpdateResultPayload says how an update went.
type AgentUpdateResultPayload struct {
	// Version is the version the agent tried to update to.
	Version string `json:"version"`
	OK      bool   `json:"ok"`
	// Error says what went wrong when OK is false.
	Error string `json:"error,omitempty"`
	// RolledBack means the new version was installed, couldn't connect,
	// and the previous one was put back.
	RolledBack bool `json:"rolledBack,omitempty"`
}

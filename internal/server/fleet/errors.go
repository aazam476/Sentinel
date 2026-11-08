// SPDX-License-Identifier: Apache-2.0

package fleet

import "errors"

// Errors returned by the service. They are never reassigned.
var (
	// ErrEnrollmentRefused: the enrollment token is wrong, expired, revoked
	// or used up. Which one is deliberately not said to the agent: that
	// would help someone guessing tokens.
	ErrEnrollmentRefused = errors.New("the enrollment token is invalid, expired, revoked or used up")
	// ErrUnknownAgent: no agent has the ID given in a handshake.
	ErrUnknownAgent = errors.New("no agent has this ID")
	// ErrAgentRevoked: the agent was revoked and may not connect.
	ErrAgentRevoked = errors.New("this agent has been revoked")
)

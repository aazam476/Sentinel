// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Outcome says how an audited action ended.
type Outcome string

// The possible outcomes.
const (
	// OutcomeSuccess means the action happened.
	OutcomeSuccess Outcome = "success"
	// OutcomeFailure means the action was attempted and failed, for example
	// a wrong password.
	OutcomeFailure Outcome = "failure"
	// OutcomeDenied means the action was refused by a permission check or a
	// machine's local policy.
	OutcomeDenied Outcome = "denied"
	// OutcomeStarted means a long-running action began; a later entry
	// records how it ended.
	OutcomeStarted Outcome = "started"
)

// Entry is what a caller records. The audit package adds the ID, time and
// hashes.
type Entry struct {
	// UserID is the user who acted, or nil when unknown (for example a
	// login attempt for a user that doesn't exist).
	UserID *int64
	// Username is the name the user had at the time, or the name that was
	// tried for a failed login. Empty for actions with no user.
	Username string
	// AgentID is the machine the action was about, or nil.
	AgentID *int64
	// Action names what happened, as "<area>.<thing>" or
	// "<area>.<object>.<verb>", for example "auth.login".
	Action string
	// Details holds action-specific facts. It is stored as a JSON object,
	// so it must marshal to one. Never put passwords, tokens or secrets here.
	Details map[string]any
	// Outcome says how the action ended.
	Outcome Outcome
	// IP is the client's address, from middleware.ClientInfoFrom.
	IP string
}

// Record is a stored audit log row.
type Record struct {
	ID           int64           `json:"id"`
	Time         int64           `json:"time"`
	UserID       *int64          `json:"userId"`
	Username     string          `json:"username"`
	AgentID      *int64          `json:"agentId"`
	Action       string          `json:"action"`
	Details      json.RawMessage `json:"details"`
	Outcome      Outcome         `json:"outcome"`
	IP           string          `json:"ip"`
	PreviousHash string          `json:"previousHash"`
	Hash         string          `json:"hash"`
}

// validate checks an entry before it is stored and returns its details as
// JSON.
func (entry Entry) validate() (json.RawMessage, error) {
	if entry.Action == "" {
		return nil, errors.New("audit entry has no action")
	}
	switch entry.Outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomeDenied, OutcomeStarted:
	default:
		return nil, fmt.Errorf("audit entry %s has unknown outcome %q", entry.Action, entry.Outcome)
	}

	details := entry.Details
	if details == nil {
		details = map[string]any{}
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("audit entry %s: encode details: %w", entry.Action, err)
	}
	return detailsJSON, nil
}

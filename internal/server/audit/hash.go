// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// genesisHash is the "previous hash" of the very first row: 64 zeros, the
// length of a hex SHA-256.
const genesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// hashableRow is exactly what a row's hash covers, in a fixed field order.
// JSON gives an unambiguous encoding: no field's content can be mistaken
// for the boundary between two fields. Details is hashed as the exact text
// stored, not re-encoded, so any change to it shows. Changing this struct would make
// every existing row fail verification, so it must never change.
type hashableRow struct {
	ID           int64   `json:"id"`
	Time         int64   `json:"ts"`
	UserID       *int64  `json:"user_id"`
	Username     string  `json:"username"`
	AgentID      *int64  `json:"agent_id"`
	Action       string  `json:"action"`
	Details      string  `json:"details"`
	Outcome      Outcome `json:"outcome"`
	IP           string  `json:"ip"`
	PreviousHash string  `json:"previous_hash"`
}

// computeHash returns the hex SHA-256 of a record's content and previous
// hash. The record's own Hash field is ignored.
func computeHash(record Record) string {
	// Marshaling a struct of strings, integers and integer pointers can't fail.
	encoded, _ := json.Marshal(hashableRow{
		ID:           record.ID,
		Time:         record.Time,
		UserID:       record.UserID,
		Username:     record.Username,
		AgentID:      record.AgentID,
		Action:       record.Action,
		Details:      string(record.Details),
		Outcome:      record.Outcome,
		IP:           record.IP,
		PreviousHash: record.PreviousHash,
	})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

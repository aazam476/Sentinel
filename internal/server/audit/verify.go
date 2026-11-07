// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"errors"
	"fmt"
)

// VerifyResult is the outcome of checking the hash chain.
type VerifyResult struct {
	// Checked is how many entries were examined.
	Checked int64
	// Valid is true when every entry's hash and link are correct.
	Valid bool
	// BrokenAtID is the first entry that fails, when Valid is false.
	BrokenAtID int64
	// Problem describes what is wrong with that entry, when Valid is false.
	Problem string
}

// errChainBroken stops the walk over the log at the first bad entry. It is
// never reassigned.
var errChainBroken = errors.New("audit chain broken")

// Verify walks the whole log, oldest first, and checks that every entry
// links to the one before it and that its hash matches its content.
func (l *Log) Verify(ctx context.Context) (VerifyResult, error) {
	result := VerifyResult{Valid: true}
	expected := Record{ID: 0, Hash: genesisHash}

	err := l.All(ctx, func(record Record) error {
		result.Checked++
		if problem := checkLink(expected, record); problem != "" {
			result.Valid = false
			result.BrokenAtID = record.ID
			result.Problem = problem
			return errChainBroken
		}
		expected = record
		return nil
	})
	if err != nil && !errors.Is(err, errChainBroken) {
		return VerifyResult{}, err
	}
	return result, nil
}

// checkLink compares a record with the one expected to come before it and
// returns what is wrong, or "" when it is correct.
func checkLink(previous, record Record) string {
	if record.ID != previous.ID+1 {
		return fmt.Sprintf("entry %d follows entry %d; an entry is missing or out of order", record.ID, previous.ID)
	}
	if record.PreviousHash != previous.Hash {
		return fmt.Sprintf("entry %d does not link to entry %d; an earlier entry was changed or removed", record.ID, previous.ID)
	}
	if computeHash(record) != record.Hash {
		return fmt.Sprintf("entry %d was changed after it was written", record.ID)
	}
	return ""
}

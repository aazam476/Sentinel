// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"errors"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Test pattern size: a common laptop screen.
const (
	testPatternWidth  = 1280
	testPatternHeight = 720
)

// ErrUnavailable means there is no desktop to share now.
var ErrUnavailable = errors.New("no one is logged in to a graphical session")

// Provider finds the desktop to share.
type Provider struct {
	// TestPattern shares the test pattern (the policy's
	// desktop.test_pattern).
	TestPattern bool
}

// Availability says whether a session can start now.
func (p Provider) Availability() protocol.DesktopAvailability {
	if p.TestPattern {
		return protocol.DesktopAvailability{Available: true}
	}
	return protocol.DesktopAvailability{Reason: "No one is logged in to a graphical session."}
}

// Open returns the source a new session shares.
func (p Provider) Open() (Source, error) {
	if p.TestPattern {
		return NewTestPattern(testPatternWidth, testPatternHeight), nil
	}
	return nil, ErrUnavailable
}

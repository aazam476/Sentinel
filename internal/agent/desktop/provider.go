// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"context"
	"errors"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Test pattern size: a common laptop screen.
const (
	testPatternWidth  = 1280
	testPatternHeight = 720
)

// ErrUnavailable means there is no desktop to share now.
var ErrUnavailable = errors.New("there is no desktop to share")

// Screen is the machine's own desktop on one platform (x11, ...).
type Screen interface {
	// Availability says whether there's a desktop to share now.
	Availability(ctx context.Context) protocol.DesktopAvailability
	// Open connects to it for a session.
	Open(ctx context.Context) (Source, error)
}

// Provider finds the desktop to share.
type Provider struct {
	// TestPattern shares the test pattern (the policy's
	// desktop.test_pattern) instead of the screen.
	TestPattern bool
	// Screen is this platform's desktop; nil where sharing it isn't
	// supported yet.
	Screen Screen
}

// Availability says whether a session can start now.
func (p Provider) Availability(ctx context.Context) protocol.DesktopAvailability {
	switch {
	case p.TestPattern:
		return protocol.DesktopAvailability{Available: true}
	case p.Screen == nil:
		return protocol.DesktopAvailability{Reason: "Remote desktop isn’t supported on this operating system yet."}
	default:
		return p.Screen.Availability(ctx)
	}
}

// Open returns the source a new session shares.
func (p Provider) Open(ctx context.Context) (Source, error) {
	switch {
	case p.TestPattern:
		return NewTestPattern(testPatternWidth, testPatternHeight), nil
	case p.Screen == nil:
		return nil, ErrUnavailable
	default:
		return p.Screen.Open(ctx)
	}
}

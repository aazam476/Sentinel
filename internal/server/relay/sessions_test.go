// SPDX-License-Identifier: Apache-2.0

package relay_test

import (
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/relay"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func TestSessions(t *testing.T) {
	start := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	t.Run("only the user who opened a dropped session can resume it, on the same machine", func(t *testing.T) {
		sessions := relay.NewSessions(fakeclock.New(start))
		sessions.Attached("s1", 1, 10)
		if sessions.CanResume("s1", 1, 10) {
			t.Error("a session still attached can be resumed")
		}
		sessions.Detached("s1")
		if !sessions.CanResume("s1", 1, 10) {
			t.Error("the owner can't resume a dropped session")
		}
		if sessions.CanResume("s1", 2, 10) || sessions.CanResume("s1", 1, 11) || sessions.CanResume("nope", 1, 10) {
			t.Error("someone else, another machine or an unknown session can resume")
		}
	})

	t.Run("a dropped session can be resumed for about 30 seconds", func(t *testing.T) {
		clock := fakeclock.New(start)
		sessions := relay.NewSessions(clock)
		sessions.Attached("s1", 1, 10)
		sessions.Detached("s1")
		clock.Advance(relay.ResumeWindow)
		if !sessions.CanResume("s1", 1, 10) {
			t.Error("can't resume at the end of the window")
		}
		clock.Advance(10 * time.Second)
		if sessions.CanResume("s1", 1, 10) {
			t.Error("resumed long after the window")
		}
	})

	t.Run("a closed session can't be resumed, and detaching an unknown one is harmless", func(t *testing.T) {
		sessions := relay.NewSessions(fakeclock.New(start))
		sessions.Attached("s1", 1, 10)
		sessions.Ended("s1")
		sessions.Detached("s1")
		if sessions.CanResume("s1", 1, 10) {
			t.Error("a closed session can be resumed")
		}
	})
}

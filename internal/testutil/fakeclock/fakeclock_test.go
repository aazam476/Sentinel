// SPDX-License-Identifier: Apache-2.0

package fakeclock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func testStart() time.Time {
	return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
}

func TestTimers(t *testing.T) {
	t.Run("a timer fires only once the clock reaches its deadline", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		timer := clock.NewTimer(10 * time.Second)

		clock.Advance(9 * time.Second)
		assertNotFired(t, timer.C())
		clock.Advance(time.Second)
		if got := <-timer.C(); !got.Equal(testStart().Add(10 * time.Second)) {
			t.Errorf("fired with %v, want the deadline", got)
		}
	})

	t.Run("a stopped timer never fires", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		timer := clock.NewTimer(time.Second)
		if !timer.Stop() {
			t.Error("Stop() = false for an armed timer")
		}
		if timer.Stop() {
			t.Error("second Stop() = true")
		}
		clock.Advance(time.Hour)
		assertNotFired(t, timer.C())
	})

	t.Run("reset moves the deadline and drops a value already waiting", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		timer := clock.NewTimer(time.Second)
		clock.Advance(time.Second)
		if timer.Reset(time.Minute) {
			t.Error("Reset() = true for a timer that had fired")
		}
		assertNotFired(t, timer.C())
		clock.Set(testStart().Add(2 * time.Minute))
		<-timer.C()
	})

	t.Run("a timer with no duration fires at once", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		<-clock.NewTimer(0).C()
	})
}

func TestWaitForTimers(t *testing.T) {
	t.Run("returns once enough timers are armed", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		done := make(chan error)
		go func() { done <- clock.WaitForTimers(t.Context(), 2) }()
		clock.NewTimer(time.Second)
		clock.NewTimer(time.Second)
		if err := <-done; err != nil {
			t.Errorf("WaitForTimers returned %v", err)
		}
	})

	t.Run("gives up when the context ends", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := fakeclock.New(testStart()).WaitForTimers(ctx, 1)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("WaitForTimers returned %v, want context.Canceled", err)
		}
	})
}

func assertNotFired(t *testing.T, channel <-chan time.Time) {
	t.Helper()
	select {
	case got := <-channel:
		t.Errorf("timer fired at %v, want it still waiting", got)
	default:
	}
}

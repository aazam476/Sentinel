// SPDX-License-Identifier: Apache-2.0

package clock_test

import (
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/shared/clock"
)

func TestSystem(t *testing.T) {
	t.Run("returns the current time", func(t *testing.T) {
		before := time.Now()
		got := clock.System{}.Now()
		after := time.Now()

		if got.Before(before) || got.After(after) {
			t.Errorf("Now() = %v, want a time between %v and %v", got, before, after)
		}
	})
}

func TestSystemTimer(t *testing.T) {
	t.Run("fires after its duration", func(t *testing.T) {
		timer := clock.System{}.NewTimer(time.Millisecond)
		<-timer.C()
	})

	t.Run("can be stopped and reset", func(t *testing.T) {
		timer := clock.System{}.NewTimer(time.Hour)
		if !timer.Stop() {
			t.Error("Stop() = false for an armed timer")
		}
		timer.Reset(time.Millisecond)
		<-timer.C()
	})
}

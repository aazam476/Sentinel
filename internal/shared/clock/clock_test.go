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

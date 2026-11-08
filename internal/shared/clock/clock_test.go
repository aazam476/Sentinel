// SPDX-License-Identifier: Apache-2.0

package clock_test

import (
	"context"
	"errors"
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

func TestWithTimeout(t *testing.T) {
	t.Run("ends the context with ErrTimeout when the timer fires", func(t *testing.T) {
		ctx, cancel := clock.WithTimeout(t.Context(), clock.System{}, time.Millisecond)
		defer cancel()
		<-ctx.Done()
		if !errors.Is(context.Cause(ctx), clock.ErrTimeout) {
			t.Errorf("cause = %v, want ErrTimeout", context.Cause(ctx))
		}
	})

	t.Run("cancel ends the context without a timeout", func(t *testing.T) {
		ctx, cancel := clock.WithTimeout(t.Context(), clock.System{}, time.Hour)
		cancel()
		<-ctx.Done()
		if errors.Is(context.Cause(ctx), clock.ErrTimeout) {
			t.Error("cause = ErrTimeout after cancel")
		}
	})
}

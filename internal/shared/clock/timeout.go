// SPDX-License-Identifier: Apache-2.0

package clock

import (
	"context"
	"errors"
	"time"
)

// ErrTimeout is the cause of a context ended by WithTimeout. It is never
// reassigned.
var ErrTimeout = errors.New("timed out")

// TimerMaker makes timers; System and the fake clock both do.
type TimerMaker interface {
	NewTimer(duration time.Duration) Timer
}

// WithTimeout is context.WithTimeout on clock's timers, so tests can make
// it expire without waiting. context.Cause(ctx) is ErrTimeout once it has.
func WithTimeout(parent context.Context, clock TimerMaker, duration time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	timer := clock.NewTimer(duration)
	go func() {
		select {
		case <-timer.C():
			cancel(ErrTimeout)
		case <-ctx.Done():
			timer.Stop()
		}
	}()
	// Stopping the timer here, not only in the goroutine, means it is
	// disarmed by the time cancel returns; tests that count a fake clock's
	// timers rely on that.
	return ctx, func() {
		timer.Stop()
		cancel(context.Canceled)
	}
}

// SPDX-License-Identifier: Apache-2.0

package clock

import "time"

// Timer fires once, after a duration, by sending the time on C. It is
// time.Timer behind an interface, so a fake clock can fire it in tests.
type Timer interface {
	// C is the channel the time is sent on when the timer fires.
	C() <-chan time.Time
	// Stop prevents the timer from firing. It reports whether it stopped
	// the timer (false if it had already fired or been stopped).
	Stop() bool
	// Reset makes the timer fire after duration, whether or not it already
	// fired. Since Go 1.23 no value from the earlier run is left on C.
	Reset(duration time.Duration) bool
}

// System is the real wall clock.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time {
	return time.Now()
}

// NewTimer returns a real timer that fires after duration.
func (System) NewTimer(duration time.Duration) Timer {
	return systemTimer{timer: time.NewTimer(duration)}
}

type systemTimer struct {
	timer *time.Timer
}

func (t systemTimer) C() <-chan time.Time               { return t.timer.C }
func (t systemTimer) Stop() bool                        { return t.timer.Stop() }
func (t systemTimer) Reset(duration time.Duration) bool { return t.timer.Reset(duration) }

// SPDX-License-Identifier: Apache-2.0

package fakeclock

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/aazam476/sentinel/internal/shared/clock"
)

// Clock is a fake clock. Its time only changes when a test calls Set or
// Advance, and its timers fire only then. It is safe to use from several
// goroutines.
type Clock struct {
	mutex sync.Mutex
	now   time.Time
	// armed holds the timers that haven't fired or been stopped yet.
	armed []*Timer
	// changed is closed, and replaced, whenever armed changes, so
	// WaitForTimers can wait without polling.
	changed chan struct{}
}

// New returns a fake clock set to start.
func New(start time.Time) *Clock {
	return &Clock{now: start, changed: make(chan struct{})}
}

// Now returns the fake current time.
func (c *Clock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

// Advance moves the clock forward by duration and fires every timer that
// is now due, earliest first.
func (c *Clock) Advance(duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.setLocked(c.now.Add(duration))
}

// Set moves the clock to a specific time and fires every timer that is
// now due.
func (c *Clock) Set(now time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.setLocked(now)
}

// NewTimer returns a timer that fires once the fake time reaches now +
// duration.
func (c *Clock) NewTimer(duration time.Duration) clock.Timer {
	timer := &Timer{clock: c, channel: make(chan time.Time, 1)}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.armLocked(timer, duration)
	return timer
}

// WaitForTimers blocks until at least count timers are waiting to fire,
// or ctx ends. Tests call it before Advance, to be sure the code under test
// has started waiting; otherwise Advance could run first and the timer
// would never fire.
func (c *Clock) WaitForTimers(ctx context.Context, count int) error {
	for {
		c.mutex.Lock()
		armed, changed := len(c.armed), c.changed
		c.mutex.Unlock()
		if armed >= count {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return fmt.Errorf("waiting for %d timers, %d armed: %w", count, armed, ctx.Err())
		}
	}
}

func (c *Clock) setLocked(now time.Time) {
	c.now = now
	slices.SortStableFunc(c.armed, func(a, b *Timer) int { return a.deadline.Compare(b.deadline) })
	for len(c.armed) > 0 && !c.armed[0].deadline.After(now) {
		c.fireLocked(c.armed[0])
	}
}

func (c *Clock) armLocked(timer *Timer, duration time.Duration) {
	timer.deadline = c.now.Add(duration)
	c.armed = append(c.armed, timer)
	c.notifyLocked()
	if duration <= 0 {
		c.fireLocked(timer)
	}
}

func (c *Clock) fireLocked(timer *Timer) {
	c.disarmLocked(timer)
	select {
	case timer.channel <- c.now:
	default: // a value is already waiting, as with a real timer
	}
}

// disarmLocked removes timer from armed and reports whether it was there.
func (c *Clock) disarmLocked(timer *Timer) bool {
	index := slices.Index(c.armed, timer)
	if index < 0 {
		return false
	}
	c.armed = slices.Delete(c.armed, index, index+1)
	c.notifyLocked()
	return true
}

func (c *Clock) notifyLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

// Timer is a timer of a fake Clock.
type Timer struct {
	clock    *Clock
	channel  chan time.Time
	deadline time.Time
}

// C returns the channel the timer fires on.
func (t *Timer) C() <-chan time.Time {
	return t.channel
}

// Stop disarms the timer and reports whether it was still armed.
func (t *Timer) Stop() bool {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	return t.clock.disarmLocked(t)
}

// Reset re-arms the timer to fire after duration, dropping any value that
// is waiting on C, as real timers have done since Go 1.23.
func (t *Timer) Reset(duration time.Duration) bool {
	t.clock.mutex.Lock()
	defer t.clock.mutex.Unlock()
	wasArmed := t.clock.disarmLocked(t)
	select {
	case <-t.channel:
	default:
	}
	t.clock.armLocked(t, duration)
	return wasArmed
}

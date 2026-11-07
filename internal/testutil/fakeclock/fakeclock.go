// SPDX-License-Identifier: Apache-2.0

package fakeclock

import (
	"sync"
	"time"
)

// Clock is a fake clock. Its time only changes when a test calls Set or
// Advance. It is safe to use from several goroutines.
type Clock struct {
	mutex sync.Mutex
	now   time.Time
}

// New returns a fake clock set to start.
func New(start time.Time) *Clock {
	return &Clock{now: start}
}

// Now returns the fake current time.
func (c *Clock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

// Advance moves the clock forward by duration.
func (c *Clock) Advance(duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = c.now.Add(duration)
}

// Set moves the clock to a specific time.
func (c *Clock) Set(now time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = now
}

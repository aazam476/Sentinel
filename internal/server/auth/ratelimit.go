// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"sync"
	"time"
)

// Login rate limiting (SEC-05). Every failed login or second-factor attempt
// counts against both the client's IP address and the username it tried.
const (
	// freeFailures is how many failures are allowed before backoff starts,
	// so a few typos never slow anyone down.
	freeFailures = 3
	// maxBackoff caps the wait between attempts.
	maxBackoff = 15 * time.Minute
	// lockoutFailures locks a username after this many failures in a row.
	lockoutFailures = 10
	// lockoutDuration is how long a locked username stays locked.
	lockoutDuration = 15 * time.Minute
	// forgetAfter drops the failure count after this long without a failure.
	forgetAfter = time.Hour
	// maxTrackedKeys bounds memory: past this, stale entries are cleared.
	maxTrackedKeys = 100_000
)

// Clock tells the current time.
type Clock interface {
	Now() time.Time
}

// limiter tracks failed attempts per key and says how long a key must wait
// before its next attempt. The wait doubles with each failure after the
// first freeFailures: 1 s, 2 s, 4 s, ... up to maxBackoff.
type limiter struct {
	clock    Clock
	lockout  bool
	mutex    sync.Mutex
	failures map[string]*failureRecord
}

// failureRecord is the state for one key.
type failureRecord struct {
	count       int
	lastFailure time.Time
	lockedUntil time.Time
}

// newLimiter returns a limiter. With lockout set, a key that reaches
// lockoutFailures failures is refused outright for lockoutDuration.
func newLimiter(clock Clock, lockout bool) *limiter {
	return &limiter{clock: clock, lockout: lockout, failures: map[string]*failureRecord{}}
}

// RetryAfter returns how long key must wait before its next attempt, or 0
// if it may try now.
func (l *limiter) RetryAfter(key string) time.Duration {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	record := l.failures[key]
	if record == nil {
		return 0
	}
	now := l.clock.Now()
	if now.Before(record.lockedUntil) {
		return record.lockedUntil.Sub(now)
	}
	retryAt := record.lastFailure.Add(backoffFor(record.count))
	if now.Before(retryAt) {
		return retryAt.Sub(now)
	}
	return 0
}

// RecordFailure counts a failed attempt for key. It returns true when this
// failure locked the key.
func (l *limiter) RecordFailure(key string) (locked bool) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	now := l.clock.Now()
	if len(l.failures) >= maxTrackedKeys {
		l.forgetStale(now)
	}
	record := l.failures[key]
	if record == nil || now.Sub(record.lastFailure) > forgetAfter {
		record = &failureRecord{}
		l.failures[key] = record
	}

	record.count++
	record.lastFailure = now
	if l.lockout && record.count >= lockoutFailures {
		record.lockedUntil = now.Add(lockoutDuration)
		record.count = 0
		return true
	}
	return false
}

// Reset forgets key's failures, after a successful login.
func (l *limiter) Reset(key string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	delete(l.failures, key)
}

// forgetStale drops keys with no recent failure and no active lock. The
// caller holds the mutex.
func (l *limiter) forgetStale(now time.Time) {
	for key, record := range l.failures {
		if now.Sub(record.lastFailure) > forgetAfter && !now.Before(record.lockedUntil) {
			delete(l.failures, key)
		}
	}
}

// backoffFor returns the wait required after count failures in a row: none
// for the first freeFailures, then 1 s, doubling each time, up to maxBackoff.
func backoffFor(count int) time.Duration {
	if count < freeFailures {
		return 0
	}
	wait := time.Second
	for range count - freeFailures {
		wait *= 2
		if wait >= maxBackoff {
			return maxBackoff
		}
	}
	return wait
}

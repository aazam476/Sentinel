// SPDX-License-Identifier: Apache-2.0

package core

import "time"

// Reconnect delays (§7.1).
const (
	firstRetryDelay = time.Second
	maxRetryDelay   = time.Minute
	// retryJitter spreads each delay by up to 20% either way.
	retryJitter = 0.2
)

// backoff produces the delays between reconnect attempts.
type backoff struct {
	next time.Duration
	// random returns a number in [0, 1).
	random func() float64
}

func newBackoff(random func() float64) *backoff {
	return &backoff{next: firstRetryDelay, random: random}
}

// delay returns how long to wait before the next attempt, and doubles the
// one after, up to maxRetryDelay.
func (b *backoff) delay() time.Duration {
	base := b.next
	b.next = min(b.next*2, maxRetryDelay)
	spread := 1 - retryJitter + 2*retryJitter*b.random()
	return time.Duration(float64(base) * spread)
}

// reset starts again from the first delay, after a connection worked.
func (b *backoff) reset() {
	b.next = firstRetryDelay
}

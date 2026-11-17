// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"net/netip"
	"sync"
	"time"
)

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// ThrottleLimits bounds how many refusals reach the log.
type ThrottleLimits struct {
	// PerNetwork is how often one network's refusals of an action are
	// recorded: the first, then one per PerNetwork with a count.
	PerNetwork time.Duration
	// PerMinute caps one action's rows a minute across every network.
	PerMinute int
	// MaxNetworks caps how many networks are remembered.
	MaxNetworks int
}

// DefaultThrottleLimits suit refused logins and enrollments.
func DefaultThrottleLimits() ThrottleLimits {
	return ThrottleLimits{PerNetwork: time.Minute, PerMinute: 60, MaxNetworks: 10000}
}

// Throttle decides which refused attempts are written to the audit log, so
// a client that hasn't signed in can't fill the disk with them. Each
// network's first refusal of an action is recorded; repeats are counted
// and the count goes into the next row recorded (SEC-40 keeps every
// refusal accounted for, if not each as its own row). IPv6 clients are
// grouped by /64, since one machine usually has the whole /64.
type Throttle struct {
	clock  Clock
	limits ThrottleLimits

	mutex    sync.Mutex
	networks map[string]*networkState
	minute   map[string]minuteCount
}

type networkState struct {
	recordedAt time.Time
	skipped    int
}

type minuteCount struct {
	start time.Time
	rows  int
}

// NewThrottle returns a Throttle.
func NewThrottle(clock Clock, limits ThrottleLimits) *Throttle {
	return &Throttle{clock: clock, limits: limits, networks: map[string]*networkState{}, minute: map[string]minuteCount{}}
}

// Allow says whether to record this refusal, and if so how many from the
// same network and action were held back since the last one recorded.
func (t *Throttle) Allow(action, ip string) (bool, int) {
	now := t.clock.Now()
	key := action + " " + networkOf(ip)
	t.mutex.Lock()
	defer t.mutex.Unlock()
	state, known := t.networks[key]
	if known && now.Sub(state.recordedAt) < t.limits.PerNetwork {
		state.skipped++
		return false, 0
	}
	count := t.minute[action]
	if now.Sub(count.start) >= time.Minute {
		count = minuteCount{start: now}
	}
	if count.rows >= t.limits.PerMinute {
		t.remember(key, known, now.Add(-t.limits.PerNetwork), 1)
		return false, 0
	}
	count.rows++
	t.minute[action] = count
	skipped := 0
	if known {
		skipped = state.skipped
	}
	t.remember(key, known, now, 0)
	return true, skipped
}

// remember stores a network's state, keeping at most MaxNetworks. When
// full it starts again: forgetting costs at most an extra row each.
func (t *Throttle) remember(key string, known bool, recordedAt time.Time, skipped int) {
	if !known && len(t.networks) >= t.limits.MaxNetworks {
		t.networks = map[string]*networkState{}
	}
	if state, ok := t.networks[key]; ok && skipped > 0 {
		state.recordedAt = recordedAt
		state.skipped += skipped
		return
	}
	t.networks[key] = &networkState{recordedAt: recordedAt, skipped: skipped}
}

// Networks is how many networks are remembered, for tests.
func (t *Throttle) Networks() int {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return len(t.networks)
}

// networkOf is the address, or its /64 for IPv6; anything unparseable is
// one group.
func networkOf(ip string) string {
	address, err := netip.ParseAddr(ip)
	if err != nil {
		return "unknown"
	}
	address = address.Unmap()
	if address.Is4() {
		return address.String()
	}
	prefix, _ := address.Prefix(64) // a valid IPv6 address always has a /64
	return prefix.String()
}

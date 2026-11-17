// SPDX-License-Identifier: Apache-2.0

package audit_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func TestThrottle(t *testing.T) {
	newThrottle := func() (*audit.Throttle, *fakeclock.Clock) {
		clock := fakeclock.New(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC))
		return audit.NewThrottle(clock, audit.ThrottleLimits{PerNetwork: time.Minute, PerMinute: 3, MaxNetworks: 100}), clock
	}

	t.Run("records the first refusal from a network, then counts the rest until a minute has passed", func(t *testing.T) {
		throttle, clock := newThrottle()
		if ok, skipped := throttle.Allow("auth.login", "192.0.2.1"); !ok || skipped != 0 {
			t.Fatalf("first = %v, %d", ok, skipped)
		}
		for range 5 {
			if ok, _ := throttle.Allow("auth.login", "192.0.2.1"); ok {
				t.Fatal("a repeat within the minute was recorded")
			}
		}
		clock.Advance(time.Minute)
		if ok, skipped := throttle.Allow("auth.login", "192.0.2.1"); !ok || skipped != 5 {
			t.Errorf("after a minute = %v, %d; want true, 5", ok, skipped)
		}
	})

	t.Run("treats an IPv6 /64 as one network, and keeps actions apart", func(t *testing.T) {
		throttle, _ := newThrottle()
		throttle.Allow("auth.login", "2001:db8:1:2::1")
		if ok, _ := throttle.Allow("auth.login", "2001:db8:1:2:ffff::9"); ok {
			t.Error("another address in the same /64 was recorded")
		}
		if ok, _ := throttle.Allow("agent.enroll", "2001:db8:1:2::1"); !ok {
			t.Error("a different action was held back")
		}
		if ok, _ := throttle.Allow("auth.login", "2001:db8:1:3::1"); !ok {
			t.Error("another /64 was held back")
		}
	})

	t.Run("caps how many rows an action writes a minute across all networks", func(t *testing.T) {
		throttle, clock := newThrottle()
		recorded := 0
		for index := range 50 {
			if ok, _ := throttle.Allow("auth.login", fmt.Sprintf("198.51.100.%d", index)); ok {
				recorded++
			}
		}
		if recorded != 3 {
			t.Errorf("recorded %d rows from 50 networks, want 3", recorded)
		}
		clock.Advance(time.Minute)
		if ok, skipped := throttle.Allow("auth.login", "198.51.100.10"); !ok || skipped != 1 {
			t.Errorf("a held-back network next minute = %v, %d; want true, 1", ok, skipped)
		}
	})

	t.Run("forgets networks rather than grow without bound, and reads bad addresses as one", func(t *testing.T) {
		throttle := audit.NewThrottle(fakeclock.New(time.Now()), audit.ThrottleLimits{PerNetwork: time.Minute, PerMinute: 1000, MaxNetworks: 10})
		for index := range 25 {
			throttle.Allow("auth.login", fmt.Sprintf("203.0.113.%d", index))
		}
		if got := throttle.Networks(); got > 10 {
			t.Errorf("remembers %d networks, want at most 10", got)
		}
		throttle.Allow("auth.login", "not an address")
		if ok, _ := throttle.Allow("auth.login", "also not one"); ok {
			t.Error("unparseable addresses weren't grouped together")
		}
	})
}

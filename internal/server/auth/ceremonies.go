// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// ceremonyLifetime is how long the browser has to answer a passkey request.
// Browsers time the request out after a few minutes anyway.
const ceremonyLifetime = 5 * time.Minute

// ceremonyKind says what a passkey ceremony is for.
type ceremonyKind string

const (
	ceremonyRegistration ceremonyKind = "registration"
	ceremonyAssertion    ceremonyKind = "assertion"
)

// ceremony is a passkey request waiting for the browser's answer. The
// challenge in data must come back signed, which is what stops replays.
type ceremony struct {
	kind      ceremonyKind
	data      webauthn.SessionData
	expiresAt time.Time
}

// ceremonies holds one pending passkey request per session, in memory. A
// new request replaces the session's earlier one.
type ceremonies struct {
	clock   Clock
	mutex   sync.Mutex
	pending map[int64]ceremony
}

func newCeremonies(clock Clock) *ceremonies {
	return &ceremonies{clock: clock, pending: map[int64]ceremony{}}
}

// Start remembers data as sessionID's pending ceremony of kind.
func (c *ceremonies) Start(sessionID int64, kind ceremonyKind, data webauthn.SessionData) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	now := c.clock.Now()
	for id, existing := range c.pending {
		if now.After(existing.expiresAt) {
			delete(c.pending, id)
		}
	}
	c.pending[sessionID] = ceremony{kind: kind, data: data, expiresAt: now.Add(ceremonyLifetime)}
}

// Finish returns and forgets sessionID's pending ceremony, if it is of kind
// and hasn't expired. Each challenge can be answered only once.
func (c *ceremonies) Finish(sessionID int64, kind ceremonyKind) (webauthn.SessionData, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	pending, found := c.pending[sessionID]
	delete(c.pending, sessionID)
	if !found || pending.kind != kind || c.clock.Now().After(pending.expiresAt) {
		return webauthn.SessionData{}, false
	}
	return pending.data, true
}

// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"sync"
	"time"
)

// pendingSetupLifetime is how long the first admin has between entering
// their details and confirming their authenticator app.
const pendingSetupLifetime = 15 * time.Minute

// pendingSetup is a first-admin setup that has passed the
// username and password checks and waits for the TOTP code. Nothing is
// written to the database until the code is confirmed, so an abandoned
// setup leaves no half-made admin behind.
type pendingSetup struct {
	username     string
	passwordHash string
	totpSecret   string
	expiresAt    time.Time
}

// pendingSetups holds setups in progress, in memory. A server restart
// discards them; the admin then starts again.
type pendingSetups struct {
	clock   Clock
	mutex   sync.Mutex
	pending map[string]pendingSetup
}

func newPendingSetups(clock Clock) *pendingSetups {
	return &pendingSetups{clock: clock, pending: map[string]pendingSetup{}}
}

// Add stores setup and returns the ID the client uses to complete it.
func (p *pendingSetups) Add(setup pendingSetup) string {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	now := p.clock.Now()
	for id, existing := range p.pending {
		if now.After(existing.expiresAt) {
			delete(p.pending, id)
		}
	}

	id, _ := NewToken() // only the random token is needed, not its hash
	setup.expiresAt = now.Add(pendingSetupLifetime)
	p.pending[id] = setup
	return id
}

// Get returns the unexpired setup with this ID.
func (p *pendingSetups) Get(id string) (pendingSetup, bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	setup, found := p.pending[id]
	if !found || p.clock.Now().After(setup.expiresAt) {
		return pendingSetup{}, false
	}
	return setup, true
}

// Clear forgets every pending setup, once setup is complete.
func (p *pendingSetups) Clear() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	clear(p.pending)
}

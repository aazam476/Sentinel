// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"sync"
	"time"
)

// ResumeWindow is how long after a browser drops that it may reattach to
// its terminal. The agent keeps the shell this long; the server allows a
// little more, since the two clocks start at slightly different moments.
const ResumeWindow = 30 * time.Second

// resumeSlack covers the delay between the agent's clock and ours.
const resumeSlack = 5 * time.Second

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Sessions remembers open and recently dropped terminal sessions, so only
// the user who opened one, on the same machine, can reattach to it.
type Sessions struct {
	clock   Clock
	mutex   sync.Mutex
	entries map[string]sessionEntry
}

type sessionEntry struct {
	userID, agentID int64
	// detachedAt is when the browser dropped; zero while it is attached.
	detachedAt time.Time
}

// NewSessions returns an empty registry.
func NewSessions(clock Clock) *Sessions {
	return &Sessions{clock: clock, entries: map[string]sessionEntry{}}
}

// Attached records that userID is using sessionID on agentID.
func (s *Sessions) Attached(sessionID string, userID, agentID int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.entries[sessionID] = sessionEntry{userID: userID, agentID: agentID}
}

// Detached records that the browser dropped, starting the resume window.
func (s *Sessions) Detached(sessionID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if entry, ok := s.entries[sessionID]; ok {
		entry.detachedAt = s.clock.Now()
		s.entries[sessionID] = entry
	}
}

// Ended forgets a session that was closed for good.
func (s *Sessions) Ended(sessionID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.entries, sessionID)
}

// CanResume reports whether userID may reattach to sessionID on agentID:
// it is theirs, on that machine, detached, and within the resume window.
func (s *Sessions) CanResume(sessionID string, userID, agentID int64) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.forgetExpiredLocked()
	entry, ok := s.entries[sessionID]
	return ok && entry.userID == userID && entry.agentID == agentID && !entry.detachedAt.IsZero()
}

func (s *Sessions) forgetExpiredLocked() {
	cutoff := s.clock.Now().Add(-ResumeWindow - resumeSlack)
	for id, entry := range s.entries {
		if !entry.detachedAt.IsZero() && entry.detachedAt.Before(cutoff) {
			delete(s.entries, id)
		}
	}
}

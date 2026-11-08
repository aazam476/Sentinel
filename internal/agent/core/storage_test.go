// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/storage"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/metrics"
)

// fakeStorage is a machine with one pool, tank, whose health a test can
// change.
type fakeStorage struct {
	mutex  sync.Mutex
	health string
	err    error
}

func (f *fakeStorage) setHealth(health string) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.health = health
}

func (f *fakeStorage) Overview(context.Context) (protocol.StorageOverviewPayload, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	health := f.health
	if health == "" {
		health = "ONLINE"
	}
	return protocol.StorageOverviewPayload{
		ZFS:   protocol.ZFSPresence{Installed: true},
		Pools: []protocol.ZFSPool{{Name: "tank", Health: health, SizeBytes: 100, AllocatedBytes: 40}},
	}, f.err
}

func (f *fakeStorage) PoolStatus(_ context.Context, pool string) (protocol.ZFSPool, error) {
	switch pool {
	case "tank":
		return protocol.ZFSPool{Name: "tank", Health: "ONLINE"}, nil
	case "-v":
		return protocol.ZFSPool{}, storage.ErrInvalidPool
	}
	return protocol.ZFSPool{}, storage.ErrNoSuchPool
}

func (f *fakeStorage) PoolUsage(context.Context) ([]protocol.PoolUsage, error) {
	return []protocol.PoolUsage{{Name: "tank", SizeBytes: 100, AllocatedBytes: 77}}, nil
}

func startWithStorage(t *testing.T, fake *fakeStorage, localPolicy policy.Policy) (*server, *runningAgent, <-chan hub.Event) {
	t.Helper()
	s := newServer(t)
	s.machine.Storage = fake
	events := s.subscribe()
	agent := s.startAgent(localPolicy)
	waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
	return s, agent, events
}

func TestStorageRequests(t *testing.T) {
	t.Run("answers storage.overview and zfs.pool.status", func(t *testing.T) {
		s, agent, _ := startWithStorage(t, &fakeStorage{}, policy.Policy{})
		var overview protocol.StorageOverviewPayload
		if err := s.request(t, agent.state.AgentID, protocol.ActionStorageOverview, nil, &overview); err != nil || len(overview.Pools) != 1 {
			t.Errorf("got %+v, %v", overview, err)
		}
		var pool protocol.ZFSPool
		err := s.request(t, agent.state.AgentID, protocol.ActionZFSPoolStatus, protocol.ZFSPoolStatusPayload{Pool: "tank"}, &pool)
		if err != nil || pool.Name != "tank" {
			t.Errorf("got %+v, %v", pool, err)
		}
	})

	failures := []struct {
		name   string
		action string
		pool   string
		fail   error
		want   protocol.ErrorCode
	}{
		{"says when there is no such pool", protocol.ActionZFSPoolStatus, "nope", nil, protocol.ErrorNotFound},
		{"refuses a name that can't be a pool", protocol.ActionZFSPoolStatus, "-v", nil, protocol.ErrorInvalidRequest},
		{"reports a storage view it couldn't read", protocol.ActionStorageOverview, "", errors.New("no /proc"), protocol.ErrorInternal},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			s, agent, _ := startWithStorage(t, &fakeStorage{err: tc.fail}, policy.Policy{})
			err := s.request(t, agent.state.AgentID, tc.action, protocol.ZFSPoolStatusPayload{Pool: tc.pool}, nil)
			var agentErr *protocol.Error
			if !errors.As(err, &agentErr) || agentErr.Code != tc.want {
				t.Errorf("got %v, want %s", err, tc.want)
			}
		})
	}

	t.Run("refuses a pool request that isn't valid JSON", func(t *testing.T) {
		s, agent, _ := startWithStorage(t, &fakeStorage{}, policy.Policy{})
		err := s.request(t, agent.state.AgentID, protocol.ActionZFSPoolStatus, "tank", nil)
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorInvalidRequest {
			t.Errorf("got %v", err)
		}
	})

	t.Run("SEC-22: refuses the storage view, and sends no storage health, when the local policy switches storage off", func(t *testing.T) {
		localPolicy, err := policy.Parse("[capabilities]\nstorage = false\n")
		if err != nil {
			t.Fatal(err)
		}
		s, agent, _ := startWithStorage(t, &fakeStorage{health: "DEGRADED"}, localPolicy)
		for _, action := range []string{protocol.ActionStorageOverview, protocol.ActionZFSPoolStatus} {
			err := s.request(t, agent.state.AgentID, action, protocol.ZFSPoolStatusPayload{Pool: "tank"}, nil)
			var agentErr *protocol.Error
			if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied {
				t.Errorf("%s: got %v", action, err)
			}
		}
		counts, err := s.alerts.FiringCounts(t.Context())
		if err != nil || len(counts) != 0 {
			t.Errorf("alerts %v, %v", counts, err)
		}
	})
}

func TestStorageHealth(t *testing.T) {
	t.Run("a degraded pool raises an alert on connecting, and recovering resolves it within a minute", func(t *testing.T) {
		fake := &fakeStorage{health: "DEGRADED"}
		s := newServer(t)
		s.machine.Storage = fake
		events := s.subscribe()
		// The first health can arrive before the machine's details, so this
		// waits for it directly.
		agent := s.startAgent(policy.Policy{})
		waitFor(t, events, agent.state.AgentID, hub.EventAlerts)
		stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		if counts, err := s.alerts.FiringCounts(t.Context()); err != nil || counts[stored.ID] != 1 {
			t.Fatalf("counts %v, %v", counts, err)
		}

		agent.expect(t, core.EventStorageChecked)
		fake.setHealth("ONLINE")
		// A request half way keeps the connection from looking silent when
		// the clock jumps a minute.
		agent.clock.Advance(30 * time.Second)
		if err := s.hub.Request(t.Context(), stored.ID, protocol.ActionSystemInfo, nil, nil); err != nil {
			t.Fatal(err)
		}
		agent.clock.Advance(30 * time.Second)
		waitFor(t, events, agent.state.AgentID, hub.EventAlerts)
		if counts, err := s.alerts.FiringCounts(t.Context()); err != nil || counts[stored.ID] != 0 {
			t.Errorf("counts %v, %v", counts, err)
		}
	})

	t.Run("sends pool usage with each sample, so disk % counts the pool", func(t *testing.T) {
		s, agent, events := startWithStorage(t, &fakeStorage{}, policy.Policy{})
		agent.clock.Advance(10 * time.Second)
		agent.expect(t, core.EventSampled)
		waitFor(t, events, agent.state.AgentID, hub.EventMetrics)
		stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, ok := s.metrics.Latest(stored.ID)
		if !ok || snapshot.Values[metrics.DiskPercent] != 77 {
			t.Errorf("got %+v", snapshot.Values)
		}
	})
}

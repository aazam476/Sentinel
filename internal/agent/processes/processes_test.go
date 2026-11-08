// SPDX-License-Identifier: Apache-2.0

package processes_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/processes"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// fakeSource returns its snapshots in turn and records signals.
type fakeSource struct {
	snapshots [][]processes.Snapshot
	err       error
	signals   []string
}

func (s *fakeSource) Snapshot(context.Context) ([]processes.Snapshot, error) {
	if s.err != nil {
		return nil, s.err
	}
	next := s.snapshots[0]
	s.snapshots = s.snapshots[1:]
	return next, nil
}

func (s *fakeSource) Signal(_ context.Context, pid int32, force bool) error {
	if pid == 404 {
		return processes.ErrNotFound
	}
	s.signals = append(s.signals, map[bool]string{true: "kill", false: "term"}[force])
	return nil
}

func TestList(t *testing.T) {
	t.Run("reports CPU use over the last second, not since each process started", func(t *testing.T) {
		source := &fakeSource{snapshots: [][]processes.Snapshot{
			{{PID: 900, Name: "postgres", CPUSeconds: 5000}, {PID: 12, Name: "exits", CPUSeconds: 1}},
			{{PID: 900, Name: "postgres", User: "postgres", Command: "postgres -D /var/lib/postgresql", CPUSeconds: 5000.25, MemoryBytes: 1 << 30},
				{PID: 77, Name: "new", CPUSeconds: 0.5}, {PID: 300, Name: "busy", CPUSeconds: 1.5}},
		}}
		source.snapshots[0] = append(source.snapshots[0], processes.Snapshot{PID: 300, CPUSeconds: -0.5})
		clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
		manager := processes.New(source, clock, 4242)
		result := make(chan protocol.ProcessListPayload, 1)
		go func() {
			list, err := manager.List(t.Context())
			if err != nil {
				t.Error(err)
			}
			result <- list
		}()
		if err := clock.WaitForTimers(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		clock.Advance(processes.SampleInterval)
		got := (<-result).Processes
		want := []protocol.Process{
			{PID: 77, Name: "new", CPUPercent: 50},
			{PID: 300, Name: "busy", CPUPercent: 200},
			{PID: 900, Name: "postgres", User: "postgres", Command: "postgres -D /var/lib/postgresql", CPUPercent: 25, MemoryBytes: 1 << 30},
		}
		if len(got) != len(want) {
			t.Fatalf("got %+v", got)
		}
		for i := range want {
			if math.Abs(got[i].CPUPercent-want[i].CPUPercent) > 1e-9 {
				t.Errorf("%d: CPU %v, want %v", want[i].PID, got[i].CPUPercent, want[i].CPUPercent)
			}
			got[i].CPUPercent = want[i].CPUPercent
			if got[i] != want[i] {
				t.Errorf("got %+v\nwant %+v", got[i], want[i])
			}
		}
	})

	t.Run("reports a failed snapshot", func(t *testing.T) {
		manager := processes.New(&fakeSource{err: errors.New("no /proc")}, fakeclock.New(time.Now()), 1)
		if _, err := manager.List(t.Context()); err == nil {
			t.Error("List returned nil")
		}
	})

	t.Run("stops waiting when the request is cancelled", func(t *testing.T) {
		source := &fakeSource{snapshots: [][]processes.Snapshot{{}, {}}}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := processes.New(source, fakeclock.New(time.Now()), 1).List(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestKill(t *testing.T) {
	cases := []struct {
		name    string
		request protocol.ProcessKillPayload
		want    error
		signal  string
	}{
		{"asks a process to stop", protocol.ProcessKillPayload{PID: 900}, nil, "term"},
		{"kills a process outright when forced", protocol.ProcessKillPayload{PID: 900, Force: true}, nil, "kill"},
		{"refuses the system's first process", protocol.ProcessKillPayload{PID: 1, Force: true}, processes.ErrProtected, ""},
		{"refuses PID 0 and negative PIDs", protocol.ProcessKillPayload{PID: -1}, processes.ErrProtected, ""},
		{"refuses the agent itself", protocol.ProcessKillPayload{PID: 4242}, processes.ErrProtected, ""},
		{"reports a process that has gone", protocol.ProcessKillPayload{PID: 404}, processes.ErrNotFound, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			source := &fakeSource{}
			err := processes.New(source, fakeclock.New(time.Now()), 4242).Kill(t.Context(), c.request)
			if !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
			if c.signal != "" && (len(source.signals) != 1 || source.signals[0] != c.signal) {
				t.Errorf("signals = %v, want %s", source.signals, c.signal)
			}
		})
	}
}

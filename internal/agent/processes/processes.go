// SPDX-License-Identifier: Apache-2.0

package processes

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// SampleInterval is how long List watches CPU use for. A process's CPU
// time since it started says little about what it is doing now.
const SampleInterval = time.Second

// Errors from Kill. They are never reassigned.
var (
	// ErrNotFound: no process has that PID (it may just have exited).
	ErrNotFound = errors.New("there is no process with that PID")
	// ErrProtected: the process is the system's first process or the
	// agent itself, which the agent won't end.
	ErrProtected = errors.New("the agent won't end that process")
)

// Snapshot is one process at one moment.
type Snapshot struct {
	PID         int32
	Name        string
	User        string
	Command     string
	CPUSeconds  float64
	MemoryBytes uint64
}

// Source reads and signals the machine's processes.
type Source interface {
	Snapshot(ctx context.Context) ([]Snapshot, error)
	// Signal ends a process: politely (SIGTERM), or forcibly (SIGKILL).
	Signal(ctx context.Context, pid int32, force bool) error
}

// Clock tells the time and makes timers.
type Clock interface {
	Now() time.Time
	NewTimer(duration time.Duration) clock.Timer
}

// Manager lists and ends processes.
type Manager struct {
	source Source
	clock  Clock
	// self is the agent's own PID.
	self int32
}

// New returns a Manager. self is the agent's own PID, which Kill refuses.
func New(source Source, clock Clock, self int32) *Manager {
	return &Manager{source: source, clock: clock, self: self}
}

// List takes two snapshots SampleInterval apart and reports each process
// in the second with its CPU use in between, sorted by PID.
func (m *Manager) List(ctx context.Context) (protocol.ProcessListPayload, error) {
	first, err := m.source.Snapshot(ctx)
	if err != nil {
		return protocol.ProcessListPayload{}, fmt.Errorf("list processes: %w", err)
	}
	started := m.clock.Now()
	timer := m.clock.NewTimer(SampleInterval)
	select {
	case <-ctx.Done():
		timer.Stop()
		return protocol.ProcessListPayload{}, fmt.Errorf("list processes: %w", ctx.Err())
	case <-timer.C():
	}
	second, err := m.source.Snapshot(ctx)
	if err != nil {
		return protocol.ProcessListPayload{}, fmt.Errorf("list processes: %w", err)
	}
	return protocol.ProcessListPayload{Processes: cpuUse(first, second, m.clock.Now().Sub(started))}, nil
}

// cpuUse turns two snapshots into processes with their CPU use over
// elapsed. A process that started in between counts from zero.
func cpuUse(first, second []Snapshot, elapsed time.Duration) []protocol.Process {
	before := make(map[int32]float64, len(first))
	for _, process := range first {
		before[process.PID] = process.CPUSeconds
	}
	processes := make([]protocol.Process, 0, len(second))
	for _, process := range second {
		percent := 0.0
		if elapsed > 0 {
			percent = max(0, (process.CPUSeconds-before[process.PID])/elapsed.Seconds()*100)
		}
		processes = append(processes, protocol.Process{PID: process.PID, Name: process.Name, User: process.User,
			CPUPercent: percent, MemoryBytes: process.MemoryBytes, Command: process.Command})
	}
	slices.SortFunc(processes, func(a, b protocol.Process) int { return cmp.Compare(a.PID, b.PID) })
	return processes
}

// Kill ends a process, refusing the system's first process and the agent.
func (m *Manager) Kill(ctx context.Context, request protocol.ProcessKillPayload) error {
	if request.PID <= 1 || request.PID == m.self {
		return fmt.Errorf("PID %d: %w", request.PID, ErrProtected)
	}
	return m.source.Signal(ctx, request.PID, request.Force)
}

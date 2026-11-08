// SPDX-License-Identifier: Apache-2.0

package processes

import (
	"context"
	"errors"
	"fmt"

	"github.com/shirou/gopsutil/v4/process"
)

// System reads the real machine's processes through gopsutil.
type System struct{}

// Snapshot reads every process. Details the agent may not read (another
// user's command line on macOS, for one) are left empty, and a process
// that exits while being read is left out.
func (System) Snapshot(ctx context.Context) ([]Snapshot, error) {
	all, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("read processes: %w", err)
	}
	snapshots := make([]Snapshot, 0, len(all))
	for _, p := range all {
		name, err := p.NameWithContext(ctx)
		if errors.Is(err, process.ErrorProcessNotRunning) {
			continue
		}
		snapshot := Snapshot{PID: p.Pid, Name: name}
		snapshot.User, _ = p.UsernameWithContext(ctx)   // empty if unreadable
		snapshot.Command, _ = p.CmdlineWithContext(ctx) // empty if unreadable
		if times, err := p.TimesWithContext(ctx); err == nil {
			snapshot.CPUSeconds = times.User + times.System
		}
		if memory, err := p.MemoryInfoWithContext(ctx); err == nil {
			snapshot.MemoryBytes = memory.RSS
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// Signal ends the process. On Windows both ways end it outright.
func (System) Signal(ctx context.Context, pid int32, force bool) error {
	p, err := process.NewProcessWithContext(ctx, pid)
	if errors.Is(err, process.ErrorProcessNotRunning) {
		return fmt.Errorf("PID %d: %w", pid, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("find PID %d: %w", pid, err)
	}
	if force {
		err = p.KillWithContext(ctx)
	} else {
		err = p.TerminateWithContext(ctx)
	}
	if err != nil {
		return fmt.Errorf("signal PID %d: %w", pid, err)
	}
	return nil
}

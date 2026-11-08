// SPDX-License-Identifier: Apache-2.0

package processes_test

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/processes"
)

// TestSystem reads and signals real processes.
func TestSystem(t *testing.T) {
	t.Run("lists this test's own process", func(t *testing.T) {
		snapshots, err := processes.System{}.Snapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, snapshot := range snapshots {
			if snapshot.PID == int32(os.Getpid()) { //nolint:gosec // G115: PIDs fit in 32 bits on every supported system
				if snapshot.Name == "" || snapshot.MemoryBytes == 0 {
					t.Errorf("own process = %+v", snapshot)
				}
				return
			}
		}
		t.Errorf("own PID isn't among %d processes", len(snapshots))
	})

	t.Run("ends a process, politely or by force", func(t *testing.T) {
		for _, force := range []bool{false, true} {
			child := startSleeper(t)
			if err := (processes.System{}).Signal(t.Context(), int32(child.Process.Pid), force); err != nil { //nolint:gosec // G115: as above
				t.Fatalf("force %v: %v", force, err)
			}
			// Waiting returns well before the minute is up only if the
			// process was ended. On Unix it also reports the signal; Windows
			// ends it with exit code 0, which reads as a normal exit.
			if err := child.Wait(); err == nil && runtime.GOOS != "windows" {
				t.Errorf("force %v: the process exited normally", force)
			}
		}
	})

	t.Run("reports a PID nothing has", func(t *testing.T) {
		if err := (processes.System{}).Signal(t.Context(), 2_000_000_000, false); !errors.Is(err, processes.ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})
}

// startSleeper starts a process that waits for a minute.
func startSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(t.Context(), "sleep", "60")
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(t.Context(), "ping", "-n", "60", "127.0.0.1")
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	return command
}

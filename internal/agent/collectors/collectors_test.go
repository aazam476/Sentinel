// SPDX-License-Identifier: Apache-2.0

package collectors_test

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/collectors"
	"github.com/aazam476/sentinel/internal/protocol"
)

var errUnreadable = errors.New("unreadable")

// fakeSource returns fixed readings, or errUnreadable for every reading
// when broken is set.
type fakeSource struct {
	broken bool
}

func (f fakeSource) fail() error {
	if f.broken {
		return errUnreadable
	}
	return nil
}

func (f fakeSource) CPUPercent(context.Context) (float64, error) { return 12.5, f.fail() }
func (f fakeSource) Memory(context.Context) (uint64, uint64, error) {
	return 2 << 30, 8 << 30, f.fail()
}

func (f fakeSource) Filesystems(context.Context) ([]protocol.FilesystemUsage, error) {
	return []protocol.FilesystemUsage{{Mount: "/", Type: "ext4", UsedBytes: 1, TotalBytes: 2}}, f.fail()
}
func (f fakeSource) NetworkCounters(context.Context) (uint64, uint64, error) {
	return 100, 200, f.fail()
}
func (f fakeSource) DiskCounters(context.Context) (uint64, uint64, error) { return 300, 400, f.fail() }
func (f fakeSource) Temperatures(context.Context) ([]protocol.Temperature, error) {
	return []protocol.Temperature{{Sensor: "cpu", Celsius: 50}}, f.fail()
}
func (f fakeSource) Uptime(context.Context) (uint64, error) { return 3600, f.fail() }
func (f fakeSource) Host(context.Context) (collectors.Host, error) {
	return collectors.Host{Hostname: "nas-01", OS: "linux", Platform: "debian", CPUCores: 4}, f.fail()
}

func TestSample(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	t.Run("puts every reading into the sample, stamped with the time", func(t *testing.T) {
		sample := collectors.New(fakeSource{}, slog.New(slog.DiscardHandler)).Sample(t.Context(), now)
		if sample.Time != now.UnixMilli() || sample.CPUPercent != 12.5 || sample.MemoryTotalBytes != 8<<30 ||
			len(sample.Filesystems) != 1 || sample.NetworkSentBytes != 200 || sample.DiskWrittenBytes != 400 ||
			len(sample.Temperatures) != 1 || sample.UptimeSeconds != 3600 {
			t.Errorf("sample = %+v", sample)
		}
	})

	t.Run("leaves out readings that fail and still sends the sample", func(t *testing.T) {
		sample := collectors.New(fakeSource{broken: true}, slog.New(slog.DiscardHandler)).Sample(t.Context(), now)
		if sample.Time != now.UnixMilli() || sample.Filesystems == nil || len(sample.Filesystems) != 0 ||
			sample.Temperatures == nil || len(sample.Temperatures) != 0 {
			t.Errorf("sample = %+v, want a timestamped sample with empty lists", sample)
		}
	})
}

func TestSystemInfo(t *testing.T) {
	t.Run("describes the machine with the policy's capabilities and the agent's version", func(t *testing.T) {
		capabilities := []protocol.Capability{{Name: "terminal", Enabled: false, Reason: "Disabled by local policy"}}
		info, err := collectors.New(fakeSource{}, slog.New(slog.DiscardHandler)).SystemInfo(t.Context(), capabilities, "v0.1.0")
		if err != nil {
			t.Fatal(err)
		}
		if info.Hostname != "nas-01" || info.Arch != runtime.GOARCH || info.AgentVersion != "v0.1.0" || len(info.Capabilities) != 1 {
			t.Errorf("info = %+v", info)
		}
	})

	t.Run("reports a machine it can't describe", func(t *testing.T) {
		_, err := collectors.New(fakeSource{broken: true}, slog.New(slog.DiscardHandler)).SystemInfo(t.Context(), nil, "v1")
		if !errors.Is(err, errUnreadable) {
			t.Errorf("err = %v", err)
		}
	})
}

// TestHostSource reads this machine. Every call is read-only; the checks
// are loose because CI machines differ (containers have no sensors, for
// example).
func TestHostSource(t *testing.T) {
	source := collectors.NewHostSource()
	ctx := t.Context()

	t.Run("reports memory, uptime and the host", func(t *testing.T) {
		_, total, err := source.Memory(ctx)
		if err != nil || total == 0 {
			t.Errorf("memory total %d (%v)", total, err)
		}
		if _, err := source.Uptime(ctx); err != nil {
			t.Error(err)
		}
		host, err := source.Host(ctx)
		if err != nil || host.Hostname == "" || host.CPUCores == 0 || host.MemoryTotal == 0 {
			t.Errorf("host = %+v (%v)", host, err)
		}
	})

	t.Run("reports CPU use between 0 and 100", func(t *testing.T) {
		percent, err := source.CPUPercent(ctx)
		if err != nil || percent < 0 || percent > 100 {
			t.Errorf("CPU = %v (%v)", percent, err)
		}
	})

	t.Run("lists at least one filesystem with a size", func(t *testing.T) {
		filesystems, err := source.Filesystems(ctx)
		if err != nil || len(filesystems) == 0 || filesystems[0].TotalBytes == 0 {
			t.Errorf("filesystems = %+v (%v)", filesystems, err)
		}
	})

	t.Run("reads counters and sensors without failing hard", func(t *testing.T) {
		if _, _, err := source.NetworkCounters(ctx); err != nil {
			t.Error(err)
		}
		// Disk counters and sensors aren't available in every container
		// or VM; they may fail, but must not panic.
		_, _, _ = source.DiskCounters(ctx)
		_, _ = source.Temperatures(ctx)
	})
}

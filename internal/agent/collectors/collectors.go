// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Source reads the machine. Counters are totals since boot.
type Source interface {
	CPUPercent(ctx context.Context) (float64, error)
	Memory(ctx context.Context) (used, total uint64, err error)
	Filesystems(ctx context.Context) ([]protocol.FilesystemUsage, error)
	NetworkCounters(ctx context.Context) (received, sent uint64, err error)
	DiskCounters(ctx context.Context) (read, written uint64, err error)
	Temperatures(ctx context.Context) ([]protocol.Temperature, error)
	Uptime(ctx context.Context) (uint64, error)
	Host(ctx context.Context) (Host, error)
}

// Host describes the machine itself.
type Host struct {
	Hostname        string
	OS              string
	Platform        string
	PlatformVersion string
	KernelVersion   string
	CPUModel        string
	CPUCores        int
	MemoryTotal     uint64
	Interfaces      []protocol.NetworkInterface
}

// Collector turns a Source's readings into protocol messages.
type Collector struct {
	source Source
	logger *slog.Logger
}

// New returns a Collector reading from source.
func New(source Source, logger *slog.Logger) *Collector {
	return &Collector{source: source, logger: logger}
}

// Sample reads every metric, stamped with now.
func (c *Collector) Sample(ctx context.Context, now time.Time) protocol.MetricsSample {
	sample := protocol.MetricsSample{Time: now.UnixMilli(), Filesystems: []protocol.FilesystemUsage{}, Temperatures: []protocol.Temperature{}}
	var err error
	if sample.CPUPercent, err = c.source.CPUPercent(ctx); err != nil {
		c.skipped("cpu", err)
	}
	if sample.MemoryUsedBytes, sample.MemoryTotalBytes, err = c.source.Memory(ctx); err != nil {
		c.skipped("memory", err)
	}
	if filesystems, err := c.source.Filesystems(ctx); err != nil {
		c.skipped("filesystems", err)
	} else {
		sample.Filesystems = filesystems
	}
	if sample.NetworkReceivedBytes, sample.NetworkSentBytes, err = c.source.NetworkCounters(ctx); err != nil {
		c.skipped("network", err)
	}
	if sample.DiskReadBytes, sample.DiskWrittenBytes, err = c.source.DiskCounters(ctx); err != nil {
		c.skipped("disk I/O", err)
	}
	if temperatures, err := c.source.Temperatures(ctx); err != nil {
		c.skipped("temperatures", err)
	} else {
		sample.Temperatures = temperatures
	}
	if sample.UptimeSeconds, err = c.source.Uptime(ctx); err != nil {
		c.skipped("uptime", err)
	}
	return sample
}

// skipped logs a reading left out of a sample. It is debug level: a
// machine that can't report something can't, every 10 seconds.
func (c *Collector) skipped(reading string, err error) {
	c.logger.Debug("left a reading out of the metrics sample", "reading", reading, "error", err)
}

// SystemInfo describes the machine, with the capabilities its local policy
// allows and the agent's version.
func (c *Collector) SystemInfo(ctx context.Context, capabilities []protocol.Capability, agentVersion string) (protocol.SystemInfo, error) {
	host, err := c.source.Host(ctx)
	if err != nil {
		return protocol.SystemInfo{}, err
	}
	return protocol.SystemInfo{
		Hostname: host.Hostname, OS: host.OS, Platform: host.Platform, PlatformVersion: host.PlatformVersion,
		KernelVersion: host.KernelVersion, Arch: runtime.GOARCH, CPUModel: host.CPUModel, CPUCores: host.CPUCores,
		MemoryTotal: host.MemoryTotal, AgentVersion: agentVersion, Capabilities: capabilities,
		Interfaces: host.Interfaces,
	}, nil
}

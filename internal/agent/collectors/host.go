// SPDX-License-Identifier: Apache-2.0

package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"

	"github.com/aazam476/sentinel/internal/agent/storage"
	"github.com/aazam476/sentinel/internal/protocol"
)

// HostSource reads the real machine through gopsutil.
type HostSource struct{}

// NewHostSource returns the Source for this machine.
func NewHostSource() HostSource {
	return HostSource{}
}

// CPUPercent is CPU use since the previous call (gopsutil remembers the
// last reading), so calling it every 10 seconds gives each interval's use.
func (HostSource) CPUPercent(ctx context.Context) (float64, error) {
	percents, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return 0, fmt.Errorf("read CPU use: %w", err)
	}
	if len(percents) == 0 {
		return 0, nil
	}
	return percents[0], nil
}

// Memory is RAM in use and in total.
func (HostSource) Memory(ctx context.Context) (used, total uint64, err error) {
	memory, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("read memory: %w", err)
	}
	return memory.Used, memory.Total, nil
}

// Filesystems lists real, mounted filesystems, each once. Pseudo and
// system mounts (/proc, tmpfs, snaps, container layers) are left out, and
// so are ZFS datasets: their pools are sent instead (§9.5).
func (HostSource) Filesystems(ctx context.Context) ([]protocol.FilesystemUsage, error) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list filesystems: %w", err)
	}
	result := []protocol.FilesystemUsage{}
	seen := map[string]bool{}
	for _, partition := range partitions {
		if seen[partition.Mountpoint] || storage.IsZFSMount(partition.Fstype) ||
			storage.IsSystemMount(partition.Mountpoint, partition.Fstype) {
			continue
		}
		seen[partition.Mountpoint] = true
		usage, err := disk.UsageWithContext(ctx, partition.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue // unreadable or virtual; nothing to show
		}
		result = append(result, protocol.FilesystemUsage{
			Mount: partition.Mountpoint, Type: partition.Fstype, UsedBytes: usage.Used, TotalBytes: usage.Total,
		})
	}
	return result, nil
}

// NetworkCounters are bytes received and sent on every interface except
// loopback, which is the machine talking to itself.
func (HostSource) NetworkCounters(ctx context.Context) (received, sent uint64, err error) {
	interfaces, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return 0, 0, fmt.Errorf("read network counters: %w", err)
	}
	for _, counters := range interfaces {
		if counters.Name == "lo" || strings.HasPrefix(counters.Name, "Loopback") || counters.Name == "lo0" {
			continue
		}
		received += counters.BytesRecv
		sent += counters.BytesSent
	}
	return received, sent, nil
}

// DiskCounters are bytes read from and written to every disk.
func (HostSource) DiskCounters(ctx context.Context) (read, written uint64, err error) {
	disks, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("read disk counters: %w", err)
	}
	for _, counters := range disks {
		read += counters.ReadBytes
		written += counters.WriteBytes
	}
	return read, written, nil
}

// Temperatures lists sensor readings; many machines (most VMs) have none.
func (HostSource) Temperatures(ctx context.Context) ([]protocol.Temperature, error) {
	readings, err := sensors.TemperaturesWithContext(ctx)
	result := []protocol.Temperature{}
	for _, reading := range readings {
		if reading.Temperature > 0 {
			result = append(result, protocol.Temperature{Sensor: reading.SensorKey, Celsius: reading.Temperature})
		}
	}
	// gopsutil reports sensors it couldn't read as an error alongside the
	// ones it could; partial readings are still worth sending.
	if err != nil && len(result) == 0 {
		return nil, fmt.Errorf("read temperatures: %w", err)
	}
	return result, nil
}

// Uptime is seconds since boot.
func (HostSource) Uptime(ctx context.Context) (uint64, error) {
	uptime, err := host.UptimeWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("read uptime: %w", err)
	}
	return uptime, nil
}

// Host describes the machine.
func (source HostSource) Host(ctx context.Context) (Host, error) {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return Host{}, fmt.Errorf("read host details: %w", err)
	}
	result := Host{
		Hostname: info.Hostname, OS: info.OS, Platform: info.Platform,
		PlatformVersion: info.PlatformVersion, KernelVersion: info.KernelVersion,
	}
	if cpus, err := cpu.InfoWithContext(ctx); err == nil && len(cpus) > 0 {
		result.CPUModel = strings.TrimSpace(cpus[0].ModelName)
	}
	if cores, err := cpu.CountsWithContext(ctx, true); err == nil {
		result.CPUCores = cores
	}
	if _, total, err := source.Memory(ctx); err == nil {
		result.MemoryTotal = total
	}
	return result, nil
}

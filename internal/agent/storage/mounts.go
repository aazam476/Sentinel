// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/aazam476/sentinel/internal/protocol"
)

// MountSource lists mounted filesystems with their usage.
type MountSource interface {
	Mounts(ctx context.Context) ([]protocol.StorageFilesystem, error)
}

// HostMounts reads this machine's mounts through gopsutil.
type HostMounts struct{}

// Mounts lists every mount with a size, each mountpoint once. Kernel
// interfaces such as /proc have no size and are left out here.
func (HostMounts) Mounts(ctx context.Context) ([]protocol.StorageFilesystem, error) {
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("list filesystems: %w", err)
	}
	result, seen := []protocol.StorageFilesystem{}, map[string]bool{}
	for _, partition := range partitions {
		if seen[partition.Mountpoint] {
			continue
		}
		seen[partition.Mountpoint] = true
		usage, err := disk.UsageWithContext(ctx, partition.Mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}
		result = append(result, protocol.StorageFilesystem{
			Mount: partition.Mountpoint, Device: partition.Device, Type: partition.Fstype,
			UsedBytes: usage.Used, TotalBytes: usage.Total,
		})
	}
	return result, nil
}

// systemTypes are filesystem types that hold no one's data: memory-backed,
// layered or read-only images.
func systemTypes() map[string]bool {
	return map[string]bool{
		"tmpfs": true, "devtmpfs": true, "ramfs": true, "overlay": true, "aufs": true, "squashfs": true,
		"efivarfs": true, "autofs": true, "devfs": true, "nullfs": true, "fuse.lxcfs": true, "nsfs": true,
		"fuse.snapfuse": true, "fuse.portal": true, "cgroup": true, "cgroup2": true, "iso9660": true,
	}
}

// systemPaths are where systems keep mounts of their own: container
// layers, snaps, runtime state and macOS's system volumes.
func systemPaths() []string {
	return []string{
		"/var/lib/docker/", "/var/lib/containers/", "/snap/", "/run/", "/dev/", "/proc/", "/sys/",
		"/System/Volumes/", "/private/var/vm",
	}
}

// IsSystemMount says whether a mount is a pseudo or system one, hidden by
// default and never counted in the fleet's disk %.
func IsSystemMount(mount, fsType string) bool {
	if systemTypes()[fsType] {
		return true
	}
	// On macOS the read-only system is mounted at / and people's files
	// are on the Data volume, so the Data volume is the one that fills up.
	if mount == "/System/Volumes/Data" {
		return false
	}
	for _, prefix := range systemPaths() {
		if strings.HasPrefix(mount+"/", prefix) {
			return true
		}
	}
	return false
}

// IsZFSMount says whether a mount is a ZFS dataset, which the storage
// view shows with its pool instead.
func IsZFSMount(fsType string) bool {
	return fsType == "zfs"
}

// otherFilesystems drops ZFS datasets and marks system mounts.
func otherFilesystems(mounts []protocol.StorageFilesystem) []protocol.StorageFilesystem {
	result := []protocol.StorageFilesystem{}
	for _, mount := range mounts {
		if IsZFSMount(mount.Type) {
			continue
		}
		mount.System = IsSystemMount(mount.Mount, mount.Type)
		result = append(result, mount)
	}
	return result
}

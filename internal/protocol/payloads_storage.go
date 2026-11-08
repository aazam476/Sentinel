// SPDX-License-Identifier: Apache-2.0

package protocol

// Storage actions (§9.5). Everything here is read-only: the agent never
// runs a command that changes ZFS.
const (
	// ActionStorageOverview asks for the whole storage view: pools,
	// datasets, vdevs, ARC, physical disks and other filesystems.
	ActionStorageOverview = "storage.overview"
	// ActionZFSPoolStatus asks for one pool, with its vdev tree.
	ActionZFSPoolStatus = "zfs.pool.status"
	// ActionStorageHealth is the event an agent sends when its storage
	// health changes (pool state, errors, fullness, SMART), and on
	// connecting. The server raises and resolves storage alerts from it.
	ActionStorageHealth = "storage.health"
)

// StorageOverviewPayload answers storage.overview.
type StorageOverviewPayload struct {
	ZFS ZFSPresence `json:"zfs"`
	// Pools and Datasets are empty without ZFS.
	Pools    []ZFSPool    `json:"pools"`
	Datasets []ZFSDataset `json:"datasets"`
	// ARC is the ZFS cache; Linux reports it, other systems don't.
	ARC *ZFSARC `json:"arc,omitempty"`
	// Disks are the physical disks, with their pool membership.
	Disks []PhysicalDisk `json:"disks"`
	// SMARTAvailable is false when smartmontools isn't installed.
	SMARTAvailable bool `json:"smartAvailable"`
	// Filesystems are every mount except ZFS datasets.
	Filesystems []StorageFilesystem `json:"filesystems"`
}

// ZFSPresence says whether the machine has ZFS.
type ZFSPresence struct {
	Installed bool `json:"installed"`
	// Problem explains installed ZFS that can't be read, such as when
	// its kernel module isn't loaded.
	Problem string `json:"problem,omitempty"`
}

// ZFSPoolStatusPayload asks for one pool.
type ZFSPoolStatusPayload struct {
	Pool string `json:"pool"`
}

// ZFSPool is one pool, as `zpool list` and `zpool status` describe it.
type ZFSPool struct {
	Name   string `json:"name"`
	Health string `json:"health"`
	// Sizes are what `zpool list` reports, so raidz sizes include parity.
	SizeBytes      uint64 `json:"sizeBytes"`
	AllocatedBytes uint64 `json:"allocatedBytes"`
	FreeBytes      uint64 `json:"freeBytes"`
	// FragmentationPercent is absent where the pool doesn't report it.
	FragmentationPercent *uint64 `json:"fragmentationPercent,omitempty"`
	DedupRatio           float64 `json:"dedupRatio"`
	// CompressRatio is the pool's root dataset's.
	CompressRatio float64 `json:"compressRatio"`
	Scan          ZFSScan `json:"scan"`
	// Status and Action are zpool status's explanation of a problem and
	// what to do about it; both are empty for a healthy pool.
	Status string `json:"status,omitempty"`
	Action string `json:"action,omitempty"`
	// DataErrors is zpool status's errors line, e.g. "No known data errors".
	DataErrors string `json:"dataErrors"`
	// Vdevs are the pool's top-level vdevs, each section's in order.
	Vdevs []ZFSVdev `json:"vdevs"`
}

// ZFSScan is a pool's last (or running) scrub or resilver.
type ZFSScan struct {
	// Kind is "scrub", "resilver" or "none".
	Kind string `json:"kind"`
	// State is "finished", "in_progress", "canceled" or "none".
	State string `json:"state"`
	// Time is when it finished, or started if running, UTC Unix
	// milliseconds; 0 if unknown.
	Time int64 `json:"time,omitempty"`
	// Errors found by a finished scan.
	Errors uint64 `json:"errors"`
	// Text is zpool status's own description, for anything not parsed.
	Text string `json:"text"`
}

// ZFSVdev is one vdev or member device.
type ZFSVdev struct {
	// Name is the vdev ("mirror-0", "raidz2-1") or the device path.
	Name string `json:"name"`
	// Kind is "mirror", "raidz1", "raidz2", "raidz3", "draid",
	// "replacing", "spare" or "device".
	Kind string `json:"kind"`
	// Section is "data", "log", "cache", "spare", "special" or "dedup".
	Section        string `json:"section"`
	State          string `json:"state"`
	ReadErrors     uint64 `json:"readErrors"`
	WriteErrors    uint64 `json:"writeErrors"`
	ChecksumErrors uint64 `json:"checksumErrors"`
	// Note is what zpool status adds after the counts, e.g.
	// "(resilvering)" or "was /dev/sdb1".
	Note     string    `json:"note,omitempty"`
	Children []ZFSVdev `json:"children,omitempty"`
}

// ZFSDataset is one filesystem or volume (zvol), as `zfs list` reports it.
type ZFSDataset struct {
	Name string `json:"name"`
	// Type is "filesystem" or "volume".
	Type            string `json:"type"`
	UsedBytes       uint64 `json:"usedBytes"`
	AvailableBytes  uint64 `json:"availableBytes"`
	ReferencedBytes uint64 `json:"referencedBytes"`
	SnapshotBytes   uint64 `json:"snapshotBytes"`
	ChildrenBytes   uint64 `json:"childrenBytes"`
	// QuotaBytes and ReservationBytes are 0 when not set.
	QuotaBytes       uint64 `json:"quotaBytes"`
	ReservationBytes uint64 `json:"reservationBytes"`
	// VolumeSizeBytes is a volume's size; 0 for filesystems.
	VolumeSizeBytes uint64  `json:"volumeSizeBytes"`
	CompressRatio   float64 `json:"compressRatio"`
	// Mountpoint is empty for volumes and unmountable datasets.
	Mountpoint string `json:"mountpoint"`
}

// ZFSARC is the ZFS adaptive replacement cache.
type ZFSARC struct {
	SizeBytes      uint64 `json:"sizeBytes"`
	TargetMaxBytes uint64 `json:"targetMaxBytes"`
	Hits           uint64 `json:"hits"`
	Misses         uint64 `json:"misses"`
}

// PhysicalDisk is one disk, and the pool it belongs to if any.
type PhysicalDisk struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Model  string `json:"model"`
	Serial string `json:"serial"`
	// SizeBytes is 0 if unknown.
	SizeBytes uint64 `json:"sizeBytes"`
	// Kind is "ssd", "hdd", "nvme" or "unknown".
	Kind string `json:"kind"`
	// SMART is absent when it couldn't be read.
	SMART *DiskSMART `json:"smart,omitempty"`
	// Pool and Vdev name the pool the disk (or one of its partitions)
	// belongs to, e.g. "tank" and "mirror-0"; empty if none.
	Pool string `json:"pool,omitempty"`
	Vdev string `json:"vdev,omitempty"`
}

// DiskSMART is a disk's SMART health.
type DiskSMART struct {
	Passed bool `json:"passed"`
	// TemperatureCelsius is absent if the disk doesn't report one.
	TemperatureCelsius *float64 `json:"temperatureCelsius,omitempty"`
	// Failing lists attributes failing now; Warnings lists pre-fail
	// attributes that failed in the past, and NVMe critical warnings.
	Failing  []string `json:"failing"`
	Warnings []string `json:"warnings"`
}

// StorageFilesystem is one mounted filesystem other than a ZFS dataset.
type StorageFilesystem struct {
	Mount      string `json:"mount"`
	Device     string `json:"device"`
	Type       string `json:"type"`
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
	// System marks pseudo and system mounts (tmpfs, overlay, squashfs,
	// Docker layers), which the UI hides by default.
	System bool `json:"system"`
}

// PoolUsage is a pool's fullness, sent with each metrics sample so the
// fleet's disk % can count pools instead of their member disks.
type PoolUsage struct {
	Name           string `json:"name"`
	SizeBytes      uint64 `json:"sizeBytes"`
	AllocatedBytes uint64 `json:"allocatedBytes"`
}

// StorageHealthPayload is the storage.health event: what storage alerts
// are raised from. Percentages are whole numbers so that a pool filling
// slowly doesn't send an event every minute.
type StorageHealthPayload struct {
	Pools    []PoolHealth    `json:"pools"`
	Datasets []DatasetHealth `json:"datasets"`
	Disks    []DiskHealth    `json:"disks"`
}

// PoolHealth is the part of a pool that alerts depend on.
type PoolHealth struct {
	Name        string `json:"name"`
	Health      string `json:"health"`
	UsedPercent uint64 `json:"usedPercent"`
	// The error counts are totals over every vdev in the pool.
	ReadErrors     uint64 `json:"readErrors"`
	WriteErrors    uint64 `json:"writeErrors"`
	ChecksumErrors uint64 `json:"checksumErrors"`
	// ScanErrors is what the last finished scrub or resilver found.
	ScanErrors uint64 `json:"scanErrors"`
}

// DatasetHealth is a dataset with a quota, and how full it is against it.
type DatasetHealth struct {
	Name        string `json:"name"`
	UsedPercent uint64 `json:"usedPercent"`
}

// DiskHealth is a disk whose SMART could be read.
type DiskHealth struct {
	Name     string   `json:"name"`
	Model    string   `json:"model"`
	Serial   string   `json:"serial"`
	Passed   bool     `json:"passed"`
	Failing  []string `json:"failing"`
	Warnings []string `json:"warnings"`
}

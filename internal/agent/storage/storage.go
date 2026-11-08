// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Errors from Reader. They are never reassigned.
var (
	// ErrInvalidPool: the name can't be a ZFS pool's.
	ErrInvalidPool = errors.New("that isn't a valid pool name")
	// ErrNoSuchPool: no pool has that name, or ZFS isn't installed.
	ErrNoSuchPool = errors.New("there is no pool with that name")
)

// poolName is what zpool accepts as a pool name.
var poolName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]{0,254}$`)

// Options configures a Reader. Only GOOS and Runner are required.
type Options struct {
	// GOOS is the operating system, as runtime.GOOS names it.
	GOOS   string
	Runner command.Runner
	// Mounts lists filesystems; nil reads this machine's.
	Mounts MountSource
	// ReadFile reads the ARC statistics; nil uses os.ReadFile.
	ReadFile func(path string) ([]byte, error)
	// ResolvePath follows symbolic links in a pool member's path, such as
	// /dev/disk/by-id/...; nil uses filepath.EvalSymlinks.
	ResolvePath func(path string) (string, error)
	// Location is the machine's time zone, for zpool's dates; nil is local.
	Location *time.Location
	Logger   *slog.Logger
}

// Reader reads the machine's storage.
type Reader struct {
	options Options
	runner  command.Runner
}

// New returns a Reader. Every zfs and zpool command it runs is checked to
// be read-only first.
func New(options Options) *Reader {
	if options.Mounts == nil {
		options.Mounts = HostMounts{}
	}
	if options.ReadFile == nil {
		options.ReadFile = os.ReadFile
	}
	if options.ResolvePath == nil {
		options.ResolvePath = filepath.EvalSymlinks
	}
	if options.Location == nil {
		options.Location = time.Local
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	return &Reader{options: options, runner: readOnly{runner: options.Runner}}
}

// Overview reads everything the storage view shows. A part that can't be
// read is left empty rather than failing the rest.
func (r *Reader) Overview(ctx context.Context) (protocol.StorageOverviewPayload, error) {
	mounts, err := r.options.Mounts.Mounts(ctx)
	if err != nil {
		return protocol.StorageOverviewPayload{}, err
	}
	overview := protocol.StorageOverviewPayload{Filesystems: otherFilesystems(mounts)}
	overview.ZFS, overview.Pools, overview.Datasets = r.zfs(ctx)
	if overview.ZFS.Installed && r.options.GOOS == "linux" {
		if content, err := r.options.ReadFile(arcStatsPath); err == nil {
			if arc, err := ParseARCStats(content); err == nil {
				overview.ARC = &arc
			}
		}
	}
	var index DiskIndex
	overview.Disks, index, overview.SMARTAvailable = r.disks(ctx)
	r.markMembers(overview.Disks, index, overview.Pools)
	return overview, nil
}

// zfs reads pools and datasets. ZFS that is installed but can't be read
// (its kernel module isn't loaded, say) is reported as a problem.
func (r *Reader) zfs(ctx context.Context) (protocol.ZFSPresence, []protocol.ZFSPool, []protocol.ZFSDataset) {
	pools, datasets := []protocol.ZFSPool{}, []protocol.ZFSDataset{}
	listed, err := r.runner.Run(ctx, "zpool", "list", "-H", "-p", "-o", poolListColumns)
	if errors.Is(err, command.ErrNotFound) {
		return protocol.ZFSPresence{}, pools, datasets
	}
	if err != nil {
		return problem(err), pools, datasets
	}
	if pools, err = ParsePoolList(listed); err != nil {
		return problem(err), []protocol.ZFSPool{}, datasets
	}
	if len(pools) == 0 {
		return protocol.ZFSPresence{Installed: true}, pools, datasets
	}
	if err := r.addStatus(ctx, pools); err != nil {
		return problem(err), pools, datasets
	}
	output, err := r.runner.Run(ctx, "zfs", "list", "-H", "-p", "-t", "filesystem,volume", "-o", datasetColumns)
	if err == nil {
		datasets, err = ParseDatasets(output)
	}
	if err != nil {
		return problem(err), pools, []protocol.ZFSDataset{}
	}
	for i := range pools {
		pools[i].CompressRatio = 1
		for _, dataset := range datasets {
			if dataset.Name == pools[i].Name {
				pools[i].CompressRatio = dataset.CompressRatio
			}
		}
	}
	return protocol.ZFSPresence{Installed: true}, pools, datasets
}

func problem(err error) protocol.ZFSPresence {
	text, _, _ := strings.Cut(err.Error(), "\n")
	return protocol.ZFSPresence{Installed: true, Problem: text}
}

// addStatus fills in each pool's vdevs, scan and problem from zpool status.
func (r *Reader) addStatus(ctx context.Context, pools []protocol.ZFSPool, names ...string) error {
	output, err := r.runner.Run(ctx, "zpool", append([]string{"status", "-P", "-p"}, names...)...)
	if err != nil {
		return err
	}
	statuses, err := ParsePoolStatus(output, r.options.Location)
	if err != nil {
		return err
	}
	for i := range pools {
		if status, found := statuses[pools[i].Name]; found {
			pools[i].Status, pools[i].Action, pools[i].Scan = status.Status, status.Action, status.Scan
			pools[i].DataErrors, pools[i].Vdevs = status.DataErrors, status.Vdevs
		}
	}
	return nil
}

// PoolStatus reads one pool.
func (r *Reader) PoolStatus(ctx context.Context, name string) (protocol.ZFSPool, error) {
	if !poolName.MatchString(name) {
		return protocol.ZFSPool{}, ErrInvalidPool
	}
	listed, err := r.runner.Run(ctx, "zpool", "list", "-H", "-p", "-o", poolListColumns, name)
	if err != nil {
		if errors.Is(err, command.ErrNotFound) || strings.Contains(err.Error(), "no such pool") {
			return protocol.ZFSPool{}, ErrNoSuchPool
		}
		return protocol.ZFSPool{}, err
	}
	pools, err := ParsePoolList(listed)
	if err != nil {
		return protocol.ZFSPool{}, err
	}
	if len(pools) != 1 {
		return protocol.ZFSPool{}, ErrNoSuchPool
	}
	if err := r.addStatus(ctx, pools, name); err != nil {
		return protocol.ZFSPool{}, err
	}
	pools[0].CompressRatio = 1
	if output, err := r.runner.Run(ctx, "zfs", "get", "-H", "-p", "-o", "value", "compressratio", name); err == nil {
		pools[0].CompressRatio = parseRatio(strings.TrimSpace(string(output)))
	}
	return pools[0], nil
}

// PoolUsage reads how full each pool is, for the metrics sample. Without
// ZFS it returns nothing.
func (r *Reader) PoolUsage(ctx context.Context) ([]protocol.PoolUsage, error) {
	listed, err := r.runner.Run(ctx, "zpool", "list", "-H", "-p", "-o", poolListColumns)
	if errors.Is(err, command.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pools, err := ParsePoolList(listed)
	if err != nil {
		return nil, err
	}
	usage := make([]protocol.PoolUsage, 0, len(pools))
	for _, pool := range pools {
		usage = append(usage, protocol.PoolUsage{Name: pool.Name, SizeBytes: pool.SizeBytes, AllocatedBytes: pool.AllocatedBytes})
	}
	return usage, nil
}

// disks lists physical disks, with SMART where smartctl is installed,
// an index from each partition to its disk, and whether smartctl is
// installed.
func (r *Reader) disks(ctx context.Context) ([]protocol.PhysicalDisk, DiskIndex, bool) {
	disks, index := []protocol.PhysicalDisk{}, DiskIndex{}
	var err error
	switch r.options.GOOS {
	case "linux":
		var output []byte
		if output, err = r.runner.Run(ctx, "lsblk", "-J", "-b", "-o", lsblkColumns); err == nil {
			disks, index, err = ParseLsblk(output)
		}
	case "windows":
		// Windows' own disk health stands in for SMART there.
		var output []byte
		if output, err = r.runner.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", windowsDisksScript); err == nil {
			disks, err = ParseWindowsDisks(output)
		}
		return orEmpty(disks, err, r.options.Logger), index, false
	}
	disks = orEmpty(disks, err, r.options.Logger)
	if _, err := r.runner.Run(ctx, "smartctl", "--version"); err != nil {
		return disks, index, false
	}
	return r.addSMART(ctx, disks), index, true
}

func orEmpty(disks []protocol.PhysicalDisk, err error, logger *slog.Logger) []protocol.PhysicalDisk {
	if err != nil {
		logger.Warn("could not list the physical disks", "error", err)
		return []protocol.PhysicalDisk{}
	}
	return disks
}

// addSMART reads each disk's SMART. Where nothing else lists disks
// (macOS), the disks are the ones smartctl finds.
func (r *Reader) addSMART(ctx context.Context, disks []protocol.PhysicalDisk) []protocol.PhysicalDisk {
	if r.options.GOOS == "darwin" {
		if output, err := r.runner.Run(ctx, "smartctl", "--scan", "--json"); err == nil {
			paths, _ := ParseSMARTScan(output)
			for _, path := range paths {
				disks = append(disks, protocol.PhysicalDisk{Name: strings.TrimPrefix(path, "/dev/"), Path: path, Kind: "unknown"})
			}
		}
	}
	for i := range disks {
		// -n standby leaves a spun-down disk asleep rather than waking it
		// every minute to ask about its health. smartctl's exit status is
		// non-zero for many readable reports, so its output is read anyway.
		output, _ := r.runner.Run(ctx, "smartctl", "--json", "-i", "-H", "-A", "-n", "standby", disks[i].Path)
		read, err := ParseSMART(output)
		if err != nil && !errors.Is(err, errNoSMART) {
			continue
		}
		disks[i].SMART = read.SMART
		disks[i].Model, disks[i].Serial = firstNonEmpty(disks[i].Model, read.Model), firstNonEmpty(disks[i].Serial, read.Serial)
		if disks[i].SizeBytes == 0 {
			disks[i].SizeBytes = read.SizeBytes
		}
		if disks[i].Kind == "unknown" {
			disks[i].Kind = read.Kind
		}
	}
	return disks
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// diskOf is the disk a pool member is on: its path's link target, then
// the disk lsblk lists the partition under, or macOS's "disk2" in
// "/dev/disk2s1".
func (r *Reader) diskOf(path string, index DiskIndex) string {
	if resolved, err := r.options.ResolvePath(path); err == nil {
		path = resolved
	}
	name := filepath.Base(path)
	if disk, found := index[name]; found {
		return disk
	}
	if match := macDisk.FindStringSubmatch(name); match != nil {
		return match[1]
	}
	return name
}

// macDisk matches a macOS disk or partition name, such as "disk2s1".
var macDisk = regexp.MustCompile(`^(disk\d+)(s\d+)*$`)

// markMembers sets Pool and Vdev on each disk that is (or has a partition
// that is) a member of a pool.
func (r *Reader) markMembers(disks []protocol.PhysicalDisk, index DiskIndex, pools []protocol.ZFSPool) {
	members := map[string][2]string{}
	for _, pool := range pools {
		for _, vdev := range pool.Vdevs {
			label := vdevLabel(vdev)
			for _, path := range leafPaths(vdev) {
				members[r.diskOf(path, index)] = [2]string{pool.Name, label}
			}
		}
	}
	for i := range disks {
		if member, found := members[disks[i].Name]; found {
			disks[i].Pool, disks[i].Vdev = member[0], member[1]
		}
	}
}

// vdevLabel is how a member's place in its pool is described: the vdev
// it is part of ("mirror-0"), its section ("cache"), or nothing for a
// disk that is a whole data vdev by itself.
func vdevLabel(vdev protocol.ZFSVdev) string {
	if vdev.Kind != "device" {
		return vdev.Name
	}
	if vdev.Section != "data" {
		return vdev.Section
	}
	return ""
}

// leafPaths are the device paths under a vdev.
func leafPaths(vdev protocol.ZFSVdev) []string {
	if len(vdev.Children) == 0 {
		if strings.HasPrefix(vdev.Name, "/") {
			return []string{vdev.Name}
		}
		return nil
	}
	paths := []string{}
	for _, child := range vdev.Children {
		paths = append(paths, leafPaths(child)...)
	}
	return paths
}

// Health is the part of an overview that storage alerts depend on.
func Health(overview protocol.StorageOverviewPayload) protocol.StorageHealthPayload {
	health := protocol.StorageHealthPayload{
		Pools: []protocol.PoolHealth{}, Datasets: []protocol.DatasetHealth{}, Disks: []protocol.DiskHealth{},
	}
	for _, pool := range overview.Pools {
		summary := protocol.PoolHealth{
			Name: pool.Name, Health: pool.Health, UsedPercent: percentOf(pool.AllocatedBytes, pool.SizeBytes),
			ScanErrors: pool.Scan.Errors,
		}
		for _, vdev := range pool.Vdevs {
			addErrors(&summary, vdev)
		}
		health.Pools = append(health.Pools, summary)
	}
	for _, dataset := range overview.Datasets {
		if dataset.QuotaBytes > 0 {
			health.Datasets = append(health.Datasets, protocol.DatasetHealth{
				Name: dataset.Name, UsedPercent: percentOf(dataset.UsedBytes, dataset.QuotaBytes),
			})
		}
	}
	for _, disk := range overview.Disks {
		if disk.SMART != nil {
			health.Disks = append(health.Disks, protocol.DiskHealth{
				Name: disk.Name, Model: disk.Model, Serial: disk.Serial, Passed: disk.SMART.Passed,
				Failing: disk.SMART.Failing, Warnings: disk.SMART.Warnings,
			})
		}
	}
	return health
}

func addErrors(summary *protocol.PoolHealth, vdev protocol.ZFSVdev) {
	summary.ReadErrors += vdev.ReadErrors
	summary.WriteErrors += vdev.WriteErrors
	summary.ChecksumErrors += vdev.ChecksumErrors
	for _, child := range vdev.Children {
		addErrors(summary, child)
	}
}

// percentOf is part of whole as a whole percentage, rounded down.
func percentOf(part, whole uint64) uint64 {
	if whole == 0 {
		return 0
	}
	if part >= whole {
		return 100
	}
	// A float64 is exact enough for a whole percentage, and multiplying
	// two uint64s could overflow.
	return uint64(float64(part) * 100 / float64(whole))
}

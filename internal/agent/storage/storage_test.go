// SPDX-License-Identifier: Apache-2.0

package storage_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/storage"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/testutil/fakerunner"
)

// The commands the Reader runs.
const (
	zpoolList  = "zpool list -H -p -o name,size,alloc,free,frag,dedupratio,health"
	zpoolState = "zpool status -P -p"
	zfsList    = "zfs list -H -p -t filesystem,volume -o name,used,avail,refer,usedbysnapshots,usedbychildren,quota,reservation,compressratio,mountpoint,type,volsize"
	lsblk      = "lsblk -J -b -o NAME,MODEL,SERIAL,SIZE,ROTA,TRAN,TYPE"
	smartctl   = "smartctl --json -i -H -A -n standby "
)

type fakeMounts struct {
	mounts []protocol.StorageFilesystem
	err    error
}

func (f fakeMounts) Mounts(context.Context) ([]protocol.StorageFilesystem, error) {
	return f.mounts, f.err
}

func someMounts() fakeMounts {
	return fakeMounts{mounts: []protocol.StorageFilesystem{
		{Mount: "/", Device: "/dev/sda2", Type: "ext4", UsedBytes: 120e9, TotalBytes: 490e9},
		{Mount: "/tank", Device: "tank", Type: "zfs", UsedBytes: 1, TotalBytes: 2},
		{Mount: "/snap/core22/1380", Device: "/dev/loop0", Type: "squashfs", UsedBytes: 77e6, TotalBytes: 77e6},
	}}
}

// byID resolves the /dev/disk/by-id links in the fixtures as udev would.
func byID(path string) (string, error) {
	links := map[string]string{
		"/dev/disk/by-id/ata-WDC_WD20EFRX-68EUZN0_WD-WCC4M1234567": "/dev/sdb1",
		"/dev/disk/by-id/ata-WDC_WD20EFRX-68EUZN0_WD-WCC4M7654321": "/dev/sdc1",
	}
	if target, found := links[path]; found {
		return target, nil
	}
	return "", os.ErrNotExist
}

// linuxWithZFS is a Linux machine with the fixtures' pools and disks, and
// smartmontools installed.
func linuxWithZFS(t *testing.T) *fakerunner.Runner {
	t.Helper()
	runner := fakerunner.New(t).
		ExpectFile(zpoolList, "testdata/openzfs-2.2-zpool-list.txt").
		ExpectFile(zpoolState, "testdata/openzfs-2.2-zpool-status.txt").
		ExpectFile(zfsList, "testdata/openzfs-2.2-zfs-list.txt").
		ExpectFile(lsblk, "testdata/util-linux-2.39-lsblk.json").
		Expect("smartctl --version", "smartctl 7.4", nil).
		ExpectFile(smartctl+"/dev/sdb", "testdata/smartctl-7.4-sata.json").
		ExpectFile(smartctl+"/dev/nvme0n1", "testdata/smartctl-7.4-nvme.json")
	failing := golden(t, "smartctl-7.4-failing.json")
	runner.Expect(smartctl+"/dev/sdd", string(failing), errors.New("exit status 24"))
	for _, disk := range []string{"sda", "sdc", "sde", "sdf"} {
		runner.ExpectFile(smartctl+"/dev/"+disk, "testdata/smartctl-7.4-virtio.json")
	}
	return runner
}

func newReader(goos string, runner command.Runner) *storage.Reader {
	return storage.New(storage.Options{
		GOOS: goos, Runner: runner, Mounts: someMounts(), ResolvePath: byID, Location: time.UTC,
		ReadFile: func(path string) ([]byte, error) {
			if path != "/proc/spl/kstat/zfs/arcstats" {
				return nil, os.ErrNotExist
			}
			return os.ReadFile("testdata/linux-6.8-arcstats.txt")
		},
	})
}

func TestOverview(t *testing.T) {
	t.Run("shows pools with their vdevs and compression, datasets, the ARC, disks and other filesystems", func(t *testing.T) {
		overview, err := newReader("linux", linuxWithZFS(t)).Overview(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !overview.ZFS.Installed || overview.ZFS.Problem != "" || len(overview.Pools) != 2 || len(overview.Datasets) != 7 {
			t.Fatalf("got %+v", overview.ZFS)
		}
		tank := overview.Pools[1]
		if tank.CompressRatio != 1.07 || len(tank.Vdevs) != 5 || tank.Scan.State != "finished" || tank.SizeBytes != 3985729650688 {
			t.Errorf("tank: %+v", tank)
		}
		if overview.ARC == nil || overview.ARC.SizeBytes != 8320421888 {
			t.Errorf("ARC: %+v", overview.ARC)
		}
		if !overview.SMARTAvailable || len(overview.Disks) != 7 {
			t.Fatalf("disks: %+v", overview.Disks)
		}
		var mounts []string
		for _, filesystem := range overview.Filesystems {
			mounts = append(mounts, fmt.Sprintf("%s system=%v", filesystem.Mount, filesystem.System))
		}
		if want := []string{"/ system=false", "/snap/core22/1380 system=true"}; !slices.Equal(mounts, want) {
			t.Errorf("filesystems %v, want %v (ZFS datasets are shown with their pool)", mounts, want)
		}
	})

	t.Run("shows member disks as members of their pool and vdev instead of their own use", func(t *testing.T) {
		overview, err := newReader("linux", linuxWithZFS(t)).Overview(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var membership []string
		for _, disk := range overview.Disks {
			membership = append(membership, disk.Name+"="+disk.Pool+"/"+disk.Vdev)
		}
		want := []string{
			"sda=/", "sdb=tank/mirror-0", "sdc=tank/mirror-0", "sdd=tank/mirror-1", "sde=tank/mirror-1",
			"sdf=tank/spare", "nvme0n1=tank/cache",
		}
		// nvme0n1 holds both the log and the cache partitions; the later
		// section is the one named.
		if !slices.Equal(membership, want) {
			t.Errorf("got %v, want %v", membership, want)
		}
	})

	t.Run("adds SMART health to the disks that report it", func(t *testing.T) {
		overview, err := newReader("linux", linuxWithZFS(t)).Overview(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		smart := map[string]string{}
		for _, disk := range overview.Disks {
			if disk.SMART != nil {
				smart[disk.Name] = fmt.Sprintf("passed=%v failing=%v", disk.SMART.Passed, disk.SMART.Failing)
			}
		}
		want := map[string]string{
			"sdb": "passed=true failing=[]", "sdd": "passed=false failing=[Reallocated_Sector_Ct]", "nvme0n1": "passed=true failing=[]",
		}
		if fmt.Sprint(smart) != fmt.Sprint(want) {
			t.Errorf("got %v, want %v", smart, want)
		}
	})

	t.Run("shows no ZFS and no SMART on a machine without either", func(t *testing.T) {
		runner := fakerunner.New(t).
			Expect(zpoolList, "", fmt.Errorf("zpool: %w", command.ErrNotFound)).
			ExpectFile(lsblk, "testdata/util-linux-2.34-lsblk.json").
			Expect("smartctl --version", "", fmt.Errorf("smartctl: %w", command.ErrNotFound))
		overview, err := newReader("linux", runner).Overview(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if overview.ZFS.Installed || overview.ARC != nil || overview.SMARTAvailable || len(overview.Disks) != 3 || len(overview.Pools) != 0 {
			t.Errorf("got %+v", overview)
		}
	})

	t.Run("reports installed ZFS whose module isn't loaded as a problem, not an error", func(t *testing.T) {
		runner := fakerunner.New(t).
			Expect(zpoolList, "", fmt.Errorf("zpool: exit status 1: %s", "The ZFS modules are not loaded.\nTry running '/sbin/modprobe zfs' as root to load them.")).
			Expect(lsblk, "", errors.New("lsblk: exit status 32")).
			Expect("smartctl --version", "", fmt.Errorf("smartctl: %w", command.ErrNotFound))
		overview, err := newReader("linux", runner).Overview(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !overview.ZFS.Installed || !strings.HasSuffix(overview.ZFS.Problem, "The ZFS modules are not loaded.") || len(overview.Disks) != 0 {
			t.Errorf("got %+v, disks %v", overview.ZFS, overview.Disks)
		}
	})

	t.Run("shows ZFS with no pools as installed and empty", func(t *testing.T) {
		runner := fakerunner.New(t).Expect(zpoolList, "", nil).Expect(lsblk, `{"blockdevices":[]}`, nil).
			Expect("smartctl --version", "", fmt.Errorf("smartctl: %w", command.ErrNotFound))
		overview, err := newReader("linux", runner).Overview(t.Context())
		if err != nil || !overview.ZFS.Installed || len(overview.Pools) != 0 {
			t.Errorf("got %+v, %v", overview, err)
		}
	})

	t.Run("fails only when the filesystems can't be listed", func(t *testing.T) {
		reader := storage.New(storage.Options{GOOS: "linux", Runner: fakerunner.New(t), Mounts: fakeMounts{err: errors.New("no /proc")}})
		if _, err := reader.Overview(t.Context()); err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("lists Windows disks with Windows' own health and no ZFS", func(t *testing.T) {
		runner := fakerunner.New(t).Expect(zpoolList, "", fmt.Errorf("zpool: %w", command.ErrNotFound))
		data := golden(t, "windows-11-get-physicaldisk.json")
		runner.Expect("powershell -NoProfile -NonInteractive -Command "+windowsScript(t), string(data), nil)
		overview, err := newReader("windows", runner).Overview(t.Context())
		if err != nil || len(overview.Disks) != 3 || overview.SMARTAvailable || overview.Disks[0].SMART == nil {
			t.Errorf("got %+v, %v", overview, err)
		}
	})

	t.Run("lists macOS disks from smartctl, the only source of them there", func(t *testing.T) {
		runner := fakerunner.New(t).Expect(zpoolList, "", fmt.Errorf("zpool: %w", command.ErrNotFound)).
			Expect("smartctl --version", "smartctl 7.4", nil).
			ExpectFile("smartctl --scan --json", "testdata/smartctl-7.4-scan-macos.json").
			ExpectFile(smartctl+"/dev/disk0", "testdata/smartctl-7.4-nvme.json")
		overview, err := newReader("darwin", runner).Overview(t.Context())
		if err != nil || len(overview.Disks) != 1 {
			t.Fatalf("got %+v, %v", overview, err)
		}
		if disk := overview.Disks[0]; disk.Model != "Samsung SSD 980 PRO 1TB" || disk.Kind != "nvme" || disk.SizeBytes != 1000204886016 {
			t.Errorf("got %+v", disk)
		}
	})
}

// windowsScript is the PowerShell the Reader runs, read from what it
// passed to a runner that accepts anything.
func windowsScript(t *testing.T) string {
	t.Helper()
	recorder := &recordingRunner{}
	_, _ = storage.New(storage.Options{GOOS: "windows", Runner: recorder, Mounts: fakeMounts{}}).Overview(t.Context())
	for _, call := range recorder.calls {
		if script, found := strings.CutPrefix(call, "powershell -NoProfile -NonInteractive -Command "); found {
			return script
		}
	}
	t.Fatal("no PowerShell was run")
	return ""
}

// recordingRunner accepts every command, answering with nothing.
type recordingRunner struct{ calls []string }

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return nil, nil
}

func TestPoolStatus(t *testing.T) {
	t.Run("reads one pool with its vdevs and root compression", func(t *testing.T) {
		status := golden(t, "openzfs-2.2-zpool-status.txt")
		tankOnly := status[strings.Index(string(status), "  pool: tank"):]
		runner := fakerunner.New(t).
			Expect(zpoolList+" tank", "tank\t3985729650688\t2817341710336\t1168387940352\t12\t1.07\tONLINE\n", nil).
			Expect(zpoolState+" tank", string(tankOnly), nil).
			Expect("zfs get -H -p -o value compressratio tank", "1.07\n", nil)
		pool, err := newReader("linux", runner).PoolStatus(t.Context(), "tank")
		if err != nil || pool.Name != "tank" || len(pool.Vdevs) != 5 || pool.CompressRatio != 1.07 {
			t.Errorf("got %+v, %v", pool, err)
		}
	})

	cases := []struct {
		name   string
		pool   string
		answer error
		want   error
	}{
		{"refuses a name that can't be a pool's, without running anything", "../etc", nil, storage.ErrInvalidPool},
		{"refuses an option disguised as a pool name", "-v", nil, storage.ErrInvalidPool},
		{"says when there is no such pool", "nope", errors.New("zpool: exit status 1: cannot open 'nope': no such pool"), storage.ErrNoSuchPool},
		{"says when ZFS isn't installed", "tank", command.ErrNotFound, storage.ErrNoSuchPool},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := fakerunner.New(t)
			if tc.answer != nil {
				runner.Expect(zpoolList+" "+tc.pool, "", tc.answer)
			}
			if _, err := newReader("linux", runner).PoolStatus(t.Context(), tc.pool); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPoolUsage(t *testing.T) {
	t.Run("reports each pool's size and allocation", func(t *testing.T) {
		runner := fakerunner.New(t).ExpectFile(zpoolList, "testdata/openzfs-2.2-zpool-list.txt")
		usage, err := newReader("linux", runner).PoolUsage(t.Context())
		want := []protocol.PoolUsage{
			{Name: "backup", SizeBytes: 2989297238016, AllocatedBytes: 1203547291648},
			{Name: "tank", SizeBytes: 3985729650688, AllocatedBytes: 2817341710336},
		}
		if err != nil || !slices.Equal(usage, want) {
			t.Errorf("got %v, %v", usage, err)
		}
	})
	t.Run("reports nothing without ZFS, and an error when zpool fails", func(t *testing.T) {
		runner := fakerunner.New(t).Expect(zpoolList, "", command.ErrNotFound)
		if usage, err := newReader("linux", runner).PoolUsage(t.Context()); err != nil || usage != nil {
			t.Errorf("got %v, %v", usage, err)
		}
		runner = fakerunner.New(t).Expect(zpoolList, "", errors.New("exit status 1"))
		if _, err := newReader("linux", runner).PoolUsage(t.Context()); err == nil {
			t.Error("expected an error")
		}
	})
}

// TestReadOnly is §9.5's proof that the agent never runs a ZFS command
// that changes anything.
func TestReadOnly(t *testing.T) {
	t.Run("everything the agent reads runs only list, status and get", func(t *testing.T) {
		runner := linuxWithZFS(t)
		reader := newReader("linux", runner)
		if _, err := reader.Overview(t.Context()); err != nil {
			t.Fatal(err)
		}
		runner.Expect(zpoolList+" tank", "", errors.New("no such pool")).Expect(zpoolList, "", nil)
		_, _ = reader.PoolStatus(t.Context(), "tank")
		_, _ = reader.PoolUsage(t.Context())
		for _, call := range runner.Calls() {
			fields := strings.Fields(call)
			if (fields[0] == "zfs" || fields[0] == "zpool") && !slices.Contains([]string{"list", "status", "get"}, fields[1]) {
				t.Errorf("ran %q", call)
			}
		}
	})

	writes := []string{
		"zfs destroy tank/media", "zfs set quota=1G tank", "zfs snapshot tank@now", "zfs rollback tank@then",
		"zfs create tank/new", "zfs rename tank/a tank/b", "zfs send tank@now", "zfs receive tank/b",
		"zpool scrub tank", "zpool create new /dev/sdz", "zpool destroy tank", "zpool offline tank /dev/sdb",
		"zpool online tank /dev/sdb", "zpool clear tank", "zpool export tank", "zpool import tank",
		"zpool attach tank /dev/sdb /dev/sdz", "zpool replace tank /dev/sdb", "zpool set autotrim=on tank", "zpool",
	}
	for _, write := range writes {
		t.Run("refuses to run "+write, func(t *testing.T) {
			// The fake fails the test if anything reaches it.
			fields := strings.Fields(write)
			_, err := storage.ReadOnlyRunner(fakerunner.New(t)).Run(t.Context(), fields[0], fields[1:]...)
			if !errors.Is(err, storage.ErrNotReadOnly) {
				t.Errorf("got %v", err)
			}
		})
	}

	t.Run("lets other programs through", func(t *testing.T) {
		runner := fakerunner.New(t).Expect("lsblk -J", "{}", nil)
		if _, err := storage.ReadOnlyRunner(runner).Run(t.Context(), "lsblk", "-J"); err != nil {
			t.Error(err)
		}
	})
}

func TestHealth(t *testing.T) {
	t.Run("sums each pool's errors, rounds fullness down, and lists quota datasets and SMART disks", func(t *testing.T) {
		overview, err := newReader("linux", linuxWithZFS(t)).Overview(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		health := storage.Health(overview)
		backup := protocol.PoolHealth{
			Name: "backup", Health: "DEGRADED", UsedPercent: 40, ReadErrors: 1, ChecksumErrors: 2, ScanErrors: 3,
		}
		if health.Pools[0] != backup || health.Pools[1].UsedPercent != 70 {
			t.Errorf("pools %+v, want backup %+v", health.Pools, backup)
		}
		if len(health.Datasets) != 1 || health.Datasets[0] != (protocol.DatasetHealth{Name: "tank/home", UsedPercent: 85}) {
			t.Errorf("datasets %+v", health.Datasets)
		}
		if len(health.Disks) != 3 || health.Disks[1].Name != "sdd" || health.Disks[1].Passed {
			t.Errorf("disks %+v", health.Disks)
		}
	})

	t.Run("reports a full pool as 100% and an empty one as 0%", func(t *testing.T) {
		health := storage.Health(protocol.StorageOverviewPayload{Pools: []protocol.ZFSPool{
			{Name: "full", SizeBytes: 10, AllocatedBytes: 11}, {Name: "sizeless"},
		}})
		if health.Pools[0].UsedPercent != 100 || health.Pools[1].UsedPercent != 0 {
			t.Errorf("got %+v", health.Pools)
		}
	})
}

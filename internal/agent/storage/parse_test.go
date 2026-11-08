// SPDX-License-Identifier: Apache-2.0

package storage_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/storage"
	"github.com/aazam476/sentinel/internal/protocol"
)

func golden(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func uint64p(value uint64) *uint64 { return &value }

func TestParsePoolList(t *testing.T) {
	t.Run("reads each pool's size, use, fragmentation, dedup ratio and health exactly", func(t *testing.T) {
		pools, err := storage.ParsePoolList(golden(t, "openzfs-2.2-zpool-list.txt"))
		if err != nil {
			t.Fatal(err)
		}
		want := []protocol.ZFSPool{
			{
				Name: "backup", Health: "DEGRADED", SizeBytes: 2989297238016, AllocatedBytes: 1203547291648,
				FreeBytes: 1785749946368, FragmentationPercent: uint64p(3), DedupRatio: 1, Vdevs: []protocol.ZFSVdev{},
			},
			{
				Name: "tank", Health: "ONLINE", SizeBytes: 3985729650688, AllocatedBytes: 2817341710336,
				FreeBytes: 1168387940352, FragmentationPercent: uint64p(12), DedupRatio: 1.07, Vdevs: []protocol.ZFSVdev{},
			},
		}
		if !reflect.DeepEqual(pools, want) {
			t.Errorf("got %+v, want %+v", pools, want)
		}
	})

	cases := []struct {
		name    string
		output  string
		wantErr bool
		want    int
	}{
		{name: "no pools is an empty list", output: "", want: 0},
		{name: "a pool without fragmentation leaves it out", output: "p\t100\t10\t90\t-\t1.00x\tONLINE\n", want: 1},
		{name: "a line with too few columns is an error", output: "tank\t100\t10\n", wantErr: true},
		{name: "a size that isn't a number is an error", output: "p\tlots\t10\t90\t1\t1.00\tONLINE\n", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pools, err := storage.ParsePoolList([]byte(tc.output))
			if (err != nil) != tc.wantErr {
				t.Fatalf("error %v, want error %v", err, tc.wantErr)
			}
			if err == nil && len(pools) != tc.want {
				t.Errorf("got %d pools, want %d", len(pools), tc.want)
			}
			if err == nil && len(pools) == 1 && pools[0].FragmentationPercent != nil {
				t.Errorf("fragmentation %d, want none", *pools[0].FragmentationPercent)
			}
		})
	}
}

func TestParsePoolStatus(t *testing.T) {
	t.Run("reads a healthy mirror pool with logs, cache and a spare", func(t *testing.T) {
		statuses, err := storage.ParsePoolStatus(golden(t, "openzfs-2.2-zpool-status.txt"), time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		tank := statuses["tank"]
		if tank.State != "ONLINE" || tank.Status != "" || tank.DataErrors != "No known data errors" {
			t.Errorf("tank: %+v", tank)
		}
		wantScan := protocol.ZFSScan{
			Kind: "scrub", State: "finished", Errors: 0, Time: time.Date(2026, 11, 8, 0, 24, 1, 0, time.UTC).UnixMilli(),
			Text: "scrub repaired 0B in 00:41:52 with 0 errors on Sun Nov  8 00:24:01 2026",
		}
		if tank.Scan != wantScan {
			t.Errorf("scan %+v, want %+v", tank.Scan, wantScan)
		}
		var shape []string
		for _, vdev := range tank.Vdevs {
			shape = append(shape, vdev.Section+":"+vdev.Kind+":"+vdev.Name+"/"+vdev.State)
			for _, child := range vdev.Children {
				shape = append(shape, "  "+child.Name)
			}
		}
		want := []string{
			"data:mirror:mirror-0/ONLINE",
			"  /dev/disk/by-id/ata-WDC_WD20EFRX-68EUZN0_WD-WCC4M1234567",
			"  /dev/disk/by-id/ata-WDC_WD20EFRX-68EUZN0_WD-WCC4M7654321",
			"data:mirror:mirror-1/ONLINE", "  /dev/sdd", "  /dev/sde",
			"log:device:/dev/nvme0n1p4/ONLINE", "cache:device:/dev/nvme0n1p5/ONLINE", "spare:device:/dev/sdf/AVAIL",
		}
		if !reflect.DeepEqual(shape, want) {
			t.Errorf("vdevs:\n%s\nwant:\n%s", strings.Join(shape, "\n"), strings.Join(want, "\n"))
		}
	})

	t.Run("reads a degraded raidz pool's problem, errors and offline member", func(t *testing.T) {
		statuses, err := storage.ParsePoolStatus(golden(t, "openzfs-2.2-zpool-status.txt"), time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		backup := statuses["backup"]
		if backup.State != "DEGRADED" || !strings.HasPrefix(backup.Status, "One or more devices has been taken offline") ||
			!strings.HasSuffix(backup.Status, "degraded state.") || !strings.HasSuffix(backup.Action, "'zpool replace'.") {
			t.Errorf("backup: %+v", backup)
		}
		if backup.Scan.Errors != 3 || backup.DataErrors != "3 data errors, use '-v' for a list" {
			t.Errorf("scan %+v, errors %q", backup.Scan, backup.DataErrors)
		}
		raidz := backup.Vdevs[0]
		if raidz.Kind != "raidz1" || len(raidz.Children) != 3 {
			t.Fatalf("raidz: %+v", raidz)
		}
		if raidz.Children[0].ChecksumErrors != 2 || raidz.Children[1].State != "OFFLINE" || raidz.Children[2].ReadErrors != 1 {
			t.Errorf("members: %+v", raidz.Children)
		}
	})

	t.Run("reads a resilver in progress, a replacing vdev and a special mirror", func(t *testing.T) {
		statuses, err := storage.ParsePoolStatus(golden(t, "openzfs-0.8-zpool-status-resilvering.txt"), time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		tank := statuses["tank"]
		if tank.Scan.Kind != "resilver" || tank.Scan.State != "in_progress" ||
			tank.Scan.Time != time.Date(2026, 3, 3, 14, 5, 11, 0, time.UTC).UnixMilli() {
			t.Errorf("scan %+v", tank.Scan)
		}
		replacing := tank.Vdevs[0].Children[0]
		if replacing.Kind != "replacing" || len(replacing.Children) != 2 {
			t.Fatalf("replacing: %+v", replacing)
		}
		if replacing.Children[0].Note != "was /dev/sdb1" || replacing.Children[1].Note != "(resilvering)" {
			t.Errorf("notes: %+v", replacing.Children)
		}
		if special := tank.Vdevs[1]; special.Section != "special" || special.Kind != "mirror" || len(special.Children) != 2 {
			t.Errorf("special: %+v", special)
		}
	})

	scans := []struct {
		name, line, kind, state string
		errors                  uint64
	}{
		{"no scrub yet", "none requested", "none", "none", 0},
		{"a finished resilver", "resilvered 1.50M in 00:00:01 with 0 errors on Tue Mar  3 14:05:11 2026", "resilver", "finished", 0},
		{"a canceled scrub", "scrub canceled on Tue Mar  3 14:05:11 2026", "scrub", "canceled", 0},
		{"a paused scrub", "scrub paused since Tue Mar  3 14:05:11 2026", "scrub", "paused", 0},
		{"a scrub that found errors", "scrub repaired 4K in 00:01:00 with 12 errors on Tue Mar  3 14:05:11 2026", "scrub", "finished", 12},
		{"a form not understood is kept as text", "error scrub repaired 0B", "unknown", "unknown", 0},
	}
	for _, tc := range scans {
		t.Run("reads the scan line of "+tc.name, func(t *testing.T) {
			output := "  pool: p\n state: ONLINE\n  scan: " + tc.line + "\nconfig:\n\n\tNAME STATE READ WRITE CKSUM\n\tp ONLINE 0 0 0\n"
			statuses, err := storage.ParsePoolStatus([]byte(output), time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			scan := statuses["p"].Scan
			if scan.Kind != tc.kind || scan.State != tc.state || scan.Errors != tc.errors || scan.Text != tc.line {
				t.Errorf("got %+v", scan)
			}
		})
	}

	broken := []struct{ name, output string }{
		{"an unknown section heading", "  pool: p\n state: ONLINE\nconfig:\n\n\tNAME STATE READ WRITE CKSUM\n\tp ONLINE 0 0 0\n\tmystery\n"},
		{"a member indented under nothing", "  pool: p\n state: ONLINE\nconfig:\n\n\tNAME STATE READ WRITE CKSUM\n\tp ONLINE 0 0 0\n\t    /dev/sda ONLINE 0 0 0\n"},
	}
	for _, tc := range broken {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			if _, err := storage.ParsePoolStatus([]byte(tc.output), time.UTC); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestParseDatasets(t *testing.T) {
	t.Run("reads every dataset's sizes, quota, ratio and mountpoint exactly", func(t *testing.T) {
		datasets, err := storage.ParseDatasets(golden(t, "openzfs-2.2-zfs-list.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if len(datasets) != 7 {
			t.Fatalf("got %d datasets", len(datasets))
		}
		home := protocol.ZFSDataset{
			Name: "tank/home", Type: "filesystem", UsedBytes: 91268055040, AvailableBytes: 16106127360,
			ReferencedBytes: 91268055040, QuotaBytes: 107374182400, CompressRatio: 1.43, Mountpoint: "/home",
		}
		if datasets[4] != home {
			t.Errorf("got %+v, want %+v", datasets[4], home)
		}
		zvol := datasets[5]
		if zvol.Type != "volume" || zvol.VolumeSizeBytes != 53687091200 || zvol.ReservationBytes != 54760833024 || zvol.Mountpoint != "" {
			t.Errorf("zvol: %+v", zvol)
		}
		if datasets[6].Mountpoint != "" || datasets[3].SnapshotBytes != 42949672960 {
			t.Errorf("legacy %+v, photos %+v", datasets[6], datasets[3])
		}
	})

	broken := []struct{ name, line string }{
		{"a line with missing columns", "tank\t1\t2\n"},
		{"an unknown dataset type", "t\t1\t2\t3\t0\t0\t0\t0\t1.00\t/t\tsnapshot\t-\n"},
		{"a size that isn't a number", "t\tmany\t2\t3\t0\t0\t0\t0\t1.00\t/t\tfilesystem\t-\n"},
	}
	for _, tc := range broken {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			if _, err := storage.ParseDatasets([]byte(tc.line)); err == nil {
				t.Error("expected an error")
			}
		})
	}

	t.Run("reads abbreviated sizes, as zfs prints them without -p", func(t *testing.T) {
		datasets, err := storage.ParseDatasets([]byte("t\t1.50M\t2K\t512B\t0\t0\tnone\tnone\t2.10x\tnone\tfilesystem\t-\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got := datasets[0]; got.UsedBytes != 1572864 || got.AvailableBytes != 2048 || got.CompressRatio != 2.1 || got.Mountpoint != "" {
			t.Errorf("got %+v", got)
		}
	})
}

func TestParseARCStats(t *testing.T) {
	t.Run("reads the cache's size, target and hit counts", func(t *testing.T) {
		arc, err := storage.ParseARCStats(golden(t, "linux-6.8-arcstats.txt"))
		if err != nil {
			t.Fatal(err)
		}
		want := protocol.ZFSARC{SizeBytes: 8320421888, TargetMaxBytes: 16847732736, Hits: 918273645, Misses: 10293847}
		if arc != want {
			t.Errorf("got %+v, want %+v", arc, want)
		}
	})
	t.Run("refuses statistics without a size", func(t *testing.T) {
		if _, err := storage.ParseARCStats([]byte("hits 4 1\nmisses 4 1\n")); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestParseLsblk(t *testing.T) {
	t.Run("lists physical disks with their kind, leaving out zvols, RAM disks, loops and drives", func(t *testing.T) {
		disks, index, err := storage.ParseLsblk(golden(t, "util-linux-2.39-lsblk.json"))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, disk := range disks {
			names = append(names, disk.Name+":"+disk.Kind)
		}
		want := []string{"sda:ssd", "sdb:hdd", "sdc:hdd", "sdd:hdd", "sde:hdd", "sdf:hdd", "nvme0n1:nvme"}
		if !reflect.DeepEqual(names, want) {
			t.Errorf("got %v, want %v", names, want)
		}
		if disks[1].Model != "WDC WD20EFRX-68EUZN0" || disks[1].SizeBytes != 2000398934016 || disks[1].Path != "/dev/sdb" {
			t.Errorf("sdb: %+v", disks[1])
		}
		if index["sdb1"] != "sdb" || index["nvme0n1p4"] != "nvme0n1" || index["sdd"] != "sdd" {
			t.Errorf("index: %v", index)
		}
	})

	t.Run("reads older lsblk, which prints numbers as strings", func(t *testing.T) {
		disks, _, err := storage.ParseLsblk(golden(t, "util-linux-2.34-lsblk.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(disks) != 3 || disks[0].Kind != "ssd" || disks[0].SizeBytes != 500107862016 || disks[2].Kind != "hdd" {
			t.Errorf("got %+v", disks)
		}
	})

	t.Run("refuses output that isn't JSON", func(t *testing.T) {
		if _, _, err := storage.ParseLsblk([]byte("NAME SIZE")); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestParseSMART(t *testing.T) {
	cases := []struct {
		name, file, kind  string
		passed            bool
		temperature       float64
		failing, warnings []string
	}{
		{"a healthy hard disk", "smartctl-7.4-sata.json", "hdd", true, 35, []string{}, []string{}},
		{
			"a failing disk, naming the attribute failing now and the pre-fail one that failed before",
			"smartctl-7.4-failing.json", "hdd", false, 41, []string{"Reallocated_Sector_Ct"}, []string{"Spin_Retry_Count"},
		},
		{"an NVMe disk with a critical warning", "smartctl-7.4-nvme.json", "nvme", true, 44, []string{}, []string{"NVMe critical warning 0x04"}},
	}
	for _, tc := range cases {
		t.Run("reads "+tc.name, func(t *testing.T) {
			disk, err := storage.ParseSMART(golden(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			health := disk.SMART
			if disk.Kind != tc.kind || health == nil || health.Passed != tc.passed || *health.TemperatureCelsius != tc.temperature {
				t.Fatalf("got %+v %+v", disk, health)
			}
			if !reflect.DeepEqual(health.Failing, tc.failing) || !reflect.DeepEqual(health.Warnings, tc.warnings) {
				t.Errorf("failing %v warnings %v", health.Failing, health.Warnings)
			}
		})
	}

	t.Run("reports no SMART for a disk that has none, such as a virtual one", func(t *testing.T) {
		disk, err := storage.ParseSMART(golden(t, "smartctl-7.4-virtio.json"))
		if err == nil || disk.SMART != nil {
			t.Errorf("got %+v, %v", disk, err)
		}
	})

	t.Run("lists the disks smartctl finds on macOS", func(t *testing.T) {
		paths, err := storage.ParseSMARTScan(golden(t, "smartctl-7.4-scan-macos.json"))
		if err != nil || !reflect.DeepEqual(paths, []string{"/dev/disk0"}) {
			t.Errorf("got %v, %v", paths, err)
		}
	})

	t.Run("refuses output that isn't JSON", func(t *testing.T) {
		if _, err := storage.ParseSMART([]byte("smartctl 7.4")); err == nil {
			t.Error("expected an error")
		}
		if _, err := storage.ParseSMARTScan([]byte("/dev/sda -d sat")); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestParseWindowsDisks(t *testing.T) {
	t.Run("reads disks whether Windows prints enums as numbers or names", func(t *testing.T) {
		disks, err := storage.ParseWindowsDisks(golden(t, "windows-11-get-physicaldisk.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(disks) != 3 {
			t.Fatalf("got %d disks", len(disks))
		}
		nvme, hdd, virtual := disks[0], disks[1], disks[2]
		if nvme.Kind != "nvme" || nvme.Name != "PhysicalDrive0" || !nvme.SMART.Passed || *nvme.SMART.TemperatureCelsius != 38 {
			t.Errorf("nvme: %+v %+v", nvme, nvme.SMART)
		}
		if hdd.Kind != "hdd" || hdd.Serial != "ZFN0ABCD" || len(hdd.SMART.Warnings) != 1 || hdd.SMART.TemperatureCelsius != nil {
			t.Errorf("hdd: %+v %+v", hdd, hdd.SMART)
		}
		if virtual.Kind != "unknown" || virtual.SizeBytes != 137438953472 || virtual.SMART.Passed || len(virtual.SMART.Failing) != 1 {
			t.Errorf("virtual: %+v %+v", virtual, virtual.SMART)
		}
	})
	t.Run("reads no output as no disks, and refuses output that isn't JSON", func(t *testing.T) {
		if disks, err := storage.ParseWindowsDisks(nil); err != nil || len(disks) != 0 {
			t.Errorf("got %v, %v", disks, err)
		}
		if _, err := storage.ParseWindowsDisks([]byte("Get-PhysicalDisk : Access denied")); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestIsSystemMount(t *testing.T) {
	cases := []struct {
		mount, fsType string
		want          bool
	}{
		{"/", "ext4", false},
		{"/boot/efi", "vfat", false},
		{"/System/Volumes/Data", "apfs", false},
		{"/System/Volumes/VM", "apfs", true},
		{"/snap/core22/1380", "squashfs", true},
		{"/var/lib/docker/overlay2/abc/merged", "overlay", true},
		{"/run", "tmpfs", true},
		{"/dev", "devtmpfs", true},
		{"/devices", "ext4", false},
		{"C:", "NTFS", false},
	}
	for _, tc := range cases {
		t.Run(tc.mount+" "+tc.fsType, func(t *testing.T) {
			if got := storage.IsSystemMount(tc.mount, tc.fsType); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

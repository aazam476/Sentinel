// SPDX-License-Identifier: Apache-2.0

//go:build zfs

package storage_test

// The real-ZFS test (§9.5, M5's "Done when"): it needs Linux with OpenZFS
// and root, so it runs only with -tags zfs, in the nightly ZFS workflow.
// It builds test pools from sparse files, a mirror and a raidz with nested
// datasets, a quota and a zvol, and checks every number the agent reports
// against what zfs get and zpool get say; then it degrades a pool and
// checks the alert it raises.

import (
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/storage"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/alerts"
)

// recorder runs commands for real and remembers them.
type recorder struct {
	mutex sync.Mutex
	calls [][]string
}

func (r *recorder) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.mutex.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	r.mutex.Unlock()
	return command.Exec{}.Run(ctx, name, args...)
}

// zfsCmd runs a ZFS command to set up or inspect the test pools. These are
// the test's own commands, not the agent's.
func zfsCmd(t *testing.T, name string, args ...string) string {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func value(t *testing.T, name, property, target string) uint64 {
	t.Helper()
	text := zfsCmd(t, name, "get", "-H", "-p", "-o", "value", property, target)
	if text == "-" || text == "none" {
		return 0
	}
	parsed, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		t.Fatalf("%s of %s: %q", property, target, text)
	}
	return parsed
}

func createPools(t *testing.T) (mirror, raidz string) {
	t.Helper()
	dir := t.TempDir()
	files := make([]string, 5)
	for i := range files {
		files[i] = filepath.Join(dir, "disk"+strconv.Itoa(i))
		file, err := os.Create(files[i])
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(256 << 20); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
	}
	mirror, raidz = "sentinel-mirror", "sentinel-raidz"
	zfsCmd(t, "zpool", "create", "-f", "-m", filepath.Join(dir, mirror), mirror, "mirror", files[0], files[1])
	t.Cleanup(func() { _ = exec.Command("zpool", "destroy", "-f", mirror).Run() })
	zfsCmd(t, "zpool", "create", "-f", "-m", filepath.Join(dir, raidz), raidz, "raidz1", files[2], files[3], files[4])
	t.Cleanup(func() { _ = exec.Command("zpool", "destroy", "-f", raidz).Run() })

	zfsCmd(t, "zfs", "create", "-p", "-o", "compression=lz4", mirror+"/media/photos")
	zfsCmd(t, "zfs", "create", "-o", "quota=64M", mirror+"/home")
	zfsCmd(t, "zfs", "create", "-V", "32M", raidz+"/vm-disk")
	// Random data, which no compression shrinks: 56 MiB is 87% of the quota.
	data := make([]byte, 56<<20)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, mirror, "home", "data.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	zfsCmd(t, "zpool", "sync")
	return mirror, raidz
}

func TestRealZFS(t *testing.T) {
	mirror, raidz := createPools(t)
	runner := &recorder{}
	reader := storage.New(storage.Options{GOOS: "linux", Runner: runner})
	overview, err := reader.Overview(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("every pool number matches zpool get", func(t *testing.T) {
		checked := 0
		for _, pool := range overview.Pools {
			if pool.Name != mirror && pool.Name != raidz {
				continue
			}
			checked++
			for property, got := range map[string]uint64{
				"size": pool.SizeBytes, "allocated": pool.AllocatedBytes, "free": pool.FreeBytes,
			} {
				if want := value(t, "zpool", property, pool.Name); got != want {
					t.Errorf("%s %s = %d, zpool says %d", pool.Name, property, got, want)
				}
			}
			if want := zfsCmd(t, "zpool", "get", "-H", "-o", "value", "health", pool.Name); pool.Health != want {
				t.Errorf("%s health = %s, zpool says %s", pool.Name, pool.Health, want)
			}
		}
		if checked != 2 {
			t.Errorf("found %d of the 2 test pools", checked)
		}
	})

	t.Run("the vdev layout is a mirror of two and a raidz1 of three", func(t *testing.T) {
		for _, pool := range overview.Pools {
			want := map[string]string{mirror: "mirror/2", raidz: "raidz1/3"}[pool.Name]
			if want == "" {
				continue
			}
			if len(pool.Vdevs) != 1 || pool.Vdevs[0].Kind+"/"+strconv.Itoa(len(pool.Vdevs[0].Children)) != want {
				t.Errorf("%s vdevs = %+v, want %s", pool.Name, pool.Vdevs, want)
			}
		}
	})

	t.Run("every dataset number matches zfs get", func(t *testing.T) {
		checked := 0
		for _, dataset := range overview.Datasets {
			if !strings.HasPrefix(dataset.Name, "sentinel-") {
				continue
			}
			checked++
			properties := map[string]uint64{
				"used": dataset.UsedBytes, "available": dataset.AvailableBytes, "referenced": dataset.ReferencedBytes,
				"usedbysnapshots": dataset.SnapshotBytes, "usedbychildren": dataset.ChildrenBytes,
				"quota": dataset.QuotaBytes, "reservation": dataset.ReservationBytes,
			}
			if dataset.Type == "volume" {
				properties["volsize"] = dataset.VolumeSizeBytes
			}
			for property, got := range properties {
				if want := value(t, "zfs", property, dataset.Name); got != want {
					t.Errorf("%s %s = %d, zfs says %d", dataset.Name, property, got, want)
				}
			}
		}
		// The two pools' roots, media, media/photos, home and the zvol.
		if checked != 6 {
			t.Errorf("found %d of the 6 test datasets", checked)
		}
	})

	t.Run("the quota and the zvol are reported as such", func(t *testing.T) {
		found := map[string]protocol.ZFSDataset{}
		for _, dataset := range overview.Datasets {
			found[dataset.Name] = dataset
		}
		if home := found[mirror+"/home"]; home.QuotaBytes != 64<<20 || home.UsedBytes < 50<<20 {
			t.Errorf("home = %+v", home)
		}
		if zvol := found[raidz+"/vm-disk"]; zvol.Type != "volume" || zvol.VolumeSizeBytes != 32<<20 || zvol.Mountpoint != "" {
			t.Errorf("zvol = %+v", zvol)
		}
	})

	t.Run("a quota above 80% raises an alert", func(t *testing.T) {
		conditions := alerts.StorageConditions(storage.Health(overview), alerts.DefaultCapacityPercent)
		if !hasCondition(conditions, alerts.KindDatasetQuota, mirror+"/home") {
			t.Errorf("conditions = %+v", conditions)
		}
	})

	t.Run("a deliberately degraded pool raises an alert", func(t *testing.T) {
		pool, err := reader.PoolStatus(t.Context(), mirror)
		if err != nil {
			t.Fatal(err)
		}
		zfsCmd(t, "zpool", "offline", mirror, pool.Vdevs[0].Children[1].Name)
		degraded, err := reader.Overview(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conditions := alerts.StorageConditions(storage.Health(degraded), alerts.DefaultCapacityPercent)
		if !hasCondition(conditions, alerts.KindPoolHealth, mirror) {
			t.Errorf("conditions = %+v", conditions)
		}
	})

	t.Run("the agent ran only read-only zfs and zpool commands", func(t *testing.T) {
		for _, call := range runner.calls {
			if (call[0] == "zfs" || call[0] == "zpool") && call[1] != "list" && call[1] != "status" && call[1] != "get" {
				t.Errorf("ran %v", call)
			}
		}
	})
}

func hasCondition(conditions []alerts.Condition, kind, subject string) bool {
	for _, condition := range conditions {
		if condition.Kind == kind && condition.Subject == subject {
			return true
		}
	}
	return false
}

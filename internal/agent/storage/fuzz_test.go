// SPDX-License-Identifier: Apache-2.0

package storage_test

import (
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/storage"
	"github.com/aazam476/sentinel/internal/protocol"
)

// FuzzParsers checks that no output from zfs, zpool, lsblk, smartctl,
// PowerShell or the kernel makes a parser panic, and that a vdev tree
// never claims a kind or section outside the protocol's.
func FuzzParsers(f *testing.F) {
	for _, name := range []string{
		"openzfs-2.2-zpool-list.txt", "openzfs-2.2-zpool-status.txt", "openzfs-0.8-zpool-status-resilvering.txt",
		"openzfs-2.2-zfs-list.txt", "linux-6.8-arcstats.txt", "util-linux-2.39-lsblk.json", "util-linux-2.34-lsblk.json",
		"smartctl-7.4-sata.json", "smartctl-7.4-failing.json", "smartctl-7.4-scan-macos.json", "windows-11-get-physicaldisk.json",
	} {
		f.Add(golden(f, name))
	}
	kinds := map[string]bool{
		"mirror": true, "raidz1": true, "raidz2": true, "raidz3": true, "draid": true, "replacing": true, "spare": true, "device": true,
	}
	sections := map[string]bool{"data": true, "log": true, "cache": true, "spare": true, "special": true, "dedup": true}
	var check func(t *testing.T, vdev protocol.ZFSVdev)
	check = func(t *testing.T, vdev protocol.ZFSVdev) {
		if !kinds[vdev.Kind] || !sections[vdev.Section] {
			t.Errorf("vdev %+v", vdev)
		}
		for _, child := range vdev.Children {
			check(t, child)
		}
	}
	f.Fuzz(func(t *testing.T, output []byte) {
		_, _ = storage.ParsePoolList(output)
		if statuses, err := storage.ParsePoolStatus(output, time.UTC); err == nil {
			for _, status := range statuses {
				for _, vdev := range status.Vdevs {
					check(t, vdev)
				}
			}
		}
		_, _ = storage.ParseDatasets(output)
		_, _ = storage.ParseARCStats(output)
		_, _, _ = storage.ParseLsblk(output)
		_, _ = storage.ParseSMART(output)
		_, _ = storage.ParseSMARTScan(output)
		_, _ = storage.ParseWindowsDisks(output)
	})
}

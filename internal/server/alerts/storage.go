// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"fmt"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Severities.
const (
	Warning  = "warning"
	Critical = "critical"
)

// The kinds of storage alert.
const (
	KindPoolHealth     = "zfs.pool.health"
	KindPoolErrors     = "zfs.pool.errors"
	KindPoolScanErrors = "zfs.pool.scan_errors"
	KindPoolCapacity   = "zfs.pool.capacity"
	KindDatasetQuota   = "zfs.dataset.quota"
	KindDiskFailing    = "smart.failing"
	KindDiskWarning    = "smart.warning"
)

// DefaultCapacityPercent is how full a pool, or a dataset against its
// quota, gets before it raises an alert (§9.5).
const DefaultCapacityPercent = 80

// StorageKinds lists every storage alert kind: the ones a storage.health
// event raises or resolves.
func StorageKinds() []string {
	return []string{
		KindPoolHealth, KindPoolErrors, KindPoolScanErrors, KindPoolCapacity, KindDatasetQuota, KindDiskFailing, KindDiskWarning,
	}
}

// Condition is something that should be firing.
type Condition struct {
	Kind     string
	Subject  string
	Severity string
	Message  string
}

// StorageConditions are the alerts a machine's storage health calls for,
// with pools and quotas alerting above capacityPercent.
func StorageConditions(health protocol.StorageHealthPayload, capacityPercent uint64) []Condition {
	conditions := []Condition{}
	for _, pool := range health.Pools {
		conditions = append(conditions, poolConditions(pool, capacityPercent)...)
	}
	for _, dataset := range health.Datasets {
		if dataset.UsedPercent >= capacityPercent {
			conditions = append(conditions, Condition{
				Kind: KindDatasetQuota, Subject: dataset.Name, Severity: Warning,
				Message: fmt.Sprintf("Dataset %s has used %d%% of its quota.", dataset.Name, dataset.UsedPercent),
			})
		}
	}
	for _, disk := range health.Disks {
		conditions = append(conditions, diskConditions(disk)...)
	}
	return conditions
}

func poolConditions(pool protocol.PoolHealth, capacityPercent uint64) []Condition {
	var conditions []Condition
	if pool.Health != "ONLINE" {
		severity := Warning
		if pool.Health != "DEGRADED" {
			severity = Critical // FAULTED, UNAVAIL, SUSPENDED: data can't be reached
		}
		conditions = append(conditions, Condition{
			Kind: KindPoolHealth, Subject: pool.Name, Severity: severity,
			Message: fmt.Sprintf("Pool %s is %s.", pool.Name, pool.Health),
		})
	}
	if pool.ReadErrors+pool.WriteErrors+pool.ChecksumErrors > 0 {
		conditions = append(conditions, Condition{
			Kind: KindPoolErrors, Subject: pool.Name, Severity: Warning,
			Message: fmt.Sprintf("Pool %s has device errors: %d read, %d write, %d checksum.",
				pool.Name, pool.ReadErrors, pool.WriteErrors, pool.ChecksumErrors),
		})
	}
	if pool.ScanErrors > 0 {
		conditions = append(conditions, Condition{
			Kind: KindPoolScanErrors, Subject: pool.Name, Severity: Warning,
			Message: fmt.Sprintf("The last scrub of pool %s found %d errors.", pool.Name, pool.ScanErrors),
		})
	}
	if pool.UsedPercent >= capacityPercent {
		conditions = append(conditions, Condition{
			Kind: KindPoolCapacity, Subject: pool.Name, Severity: Warning,
			Message: fmt.Sprintf("Pool %s is %d%% full.", pool.Name, pool.UsedPercent),
		})
	}
	return conditions
}

func diskConditions(disk protocol.DiskHealth) []Condition {
	name := disk.Name
	if disk.Model != "" {
		name += " (" + disk.Model + ")"
	}
	var conditions []Condition
	if !disk.Passed || len(disk.Failing) > 0 {
		message := "Disk " + name + " is failing its SMART health check."
		if len(disk.Failing) > 0 {
			message = "Disk " + name + " is failing SMART attributes: " + strings.Join(disk.Failing, ", ") + "."
		}
		conditions = append(conditions, Condition{Kind: KindDiskFailing, Subject: disk.Name, Severity: Critical, Message: message})
	}
	if len(disk.Warnings) > 0 {
		conditions = append(conditions, Condition{
			Kind: KindDiskWarning, Subject: disk.Name, Severity: Warning,
			Message: "Disk " + name + " has SMART warnings: " + strings.Join(disk.Warnings, ", ") + ".",
		})
	}
	return conditions
}

// RecordStorageHealth raises and resolves a machine's storage alerts from
// its storage.health event, and returns whether any changed.
func (s *Service) RecordStorageHealth(ctx context.Context, agentID int64, health protocol.StorageHealthPayload) (bool, error) {
	return s.Reconcile(ctx, agentID, StorageKinds(), StorageConditions(health, DefaultCapacityPercent))
}

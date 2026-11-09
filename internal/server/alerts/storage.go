// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
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

// Condition is something that should be firing, and the rule it's for.
type Condition struct {
	RuleID   int64
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
// its storage.health event, by the enabled storage rules that watch it,
// and returns whether any changed. Where two rules raise the same alert,
// the first (by name) is the one it belongs to.
func (s *Service) RecordStorageHealth(ctx context.Context, agentID int64, health protocol.StorageHealthPayload) (bool, error) {
	var rules []Rule
	var target machine
	var found bool
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		if rules, err = readRules(ctx, tx, RuleStorage); err != nil {
			return err
		}
		target, found, err = readMachine(ctx, tx, agentID)
		return err
	})
	if err != nil || !found {
		return false, err
	}
	var conditions []Condition
	seen := map[string]bool{}
	for _, rule := range rules {
		if !rule.appliesTo(target) {
			continue
		}
		for _, condition := range StorageConditions(health, rule.Config.CapacityPercent) {
			if key := condition.Kind + "\x00" + condition.Subject; !seen[key] {
				seen[key] = true
				condition.RuleID = rule.ID
				conditions = append(conditions, condition)
			}
		}
	}
	return s.reconcile(ctx, scope{agentID: agentID, kinds: StorageKinds()}, conditions)
}

// KindScriptFailed is the kind of a failed scheduled script's alert.
const KindScriptFailed = "script.failed"

// RecordScheduledRun raises an alert when a scheduled script failed on a
// machine, for the script_failed rules that watch it, and resolves it
// when the schedule's next run there succeeds.
func (s *Service) RecordScheduledRun(ctx context.Context, agentID, scheduleID int64, scriptName string, failure string) error {
	var rules []Rule
	var target machine
	var found bool
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		if rules, err = readRules(ctx, tx, RuleScriptFailed); err != nil {
			return err
		}
		target, found, err = readMachine(ctx, tx, agentID)
		return err
	})
	if err != nil || !found {
		return err
	}
	in := scope{agentID: agentID, kinds: []string{KindScriptFailed}, subject: "schedule " + strconv.FormatInt(scheduleID, 10)}
	var conditions []Condition
	for _, rule := range rules {
		if failure != "" && rule.appliesTo(target) {
			conditions = []Condition{{RuleID: rule.ID, Kind: KindScriptFailed, Subject: in.subject, Severity: Warning,
				Message: fmt.Sprintf("Scheduled script %s failed: %s", scriptName, failure)}}
			break
		}
	}
	return s.reconcileAndAnnounce(ctx, target, in, conditions)
}

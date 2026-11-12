// SPDX-License-Identifier: Apache-2.0

package alerts_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// healthy is a machine whose storage needs no attention.
func healthy() protocol.StorageHealthPayload {
	return protocol.StorageHealthPayload{
		Pools:    []protocol.PoolHealth{{Name: "tank", Health: "ONLINE", UsedPercent: 42}},
		Datasets: []protocol.DatasetHealth{{Name: "tank/home", UsedPercent: 10}},
		Disks:    []protocol.DiskHealth{{Name: "sda", Model: "WDC WD20EFRX", Passed: true}},
	}
}

func describe(conditions []alerts.Condition) []string {
	result := []string{}
	for _, condition := range conditions {
		result = append(result, condition.Severity+" "+condition.Kind+" "+condition.Subject+": "+condition.Message)
	}
	return result
}

func TestStorageConditions(t *testing.T) {
	cases := []struct {
		name   string
		change func(health *protocol.StorageHealthPayload)
		want   []string
	}{
		{"raises nothing for healthy storage", func(*protocol.StorageHealthPayload) {}, []string{}},
		{
			"raises a warning for a degraded pool",
			func(h *protocol.StorageHealthPayload) { h.Pools[0].Health = "DEGRADED" },
			[]string{"warning zfs.pool.health tank: Pool tank is DEGRADED."},
		},
		{
			"raises a critical alert for a faulted pool",
			func(h *protocol.StorageHealthPayload) { h.Pools[0].Health = "FAULTED" },
			[]string{"critical zfs.pool.health tank: Pool tank is FAULTED."},
		},
		{
			"raises an alert for any device error",
			func(h *protocol.StorageHealthPayload) { h.Pools[0].ChecksumErrors = 2 },
			[]string{"warning zfs.pool.errors tank: Pool tank has device errors: 0 read, 0 write, 2 checksum."},
		},
		{
			"raises an alert for a scrub that found errors",
			func(h *protocol.StorageHealthPayload) { h.Pools[0].ScanErrors = 3 },
			[]string{"warning zfs.pool.scan_errors tank: The last scrub of pool tank found 3 errors."},
		},
		{
			"raises an alert for a pool at the threshold, and a dataset over its quota's",
			func(h *protocol.StorageHealthPayload) { h.Pools[0].UsedPercent, h.Datasets[0].UsedPercent = 80, 97 },
			[]string{
				"warning zfs.pool.capacity tank: Pool tank is 80% full.",
				"warning zfs.dataset.quota tank/home: Dataset tank/home has used 97% of its quota.",
			},
		},
		{
			"raises a critical alert for a disk failing SMART, naming the attributes",
			func(h *protocol.StorageHealthPayload) {
				h.Disks[0].Passed, h.Disks[0].Failing = false, []string{"Reallocated_Sector_Ct"}
				h.Disks[0].Warnings = []string{"Spin_Retry_Count"}
			},
			[]string{
				"critical smart.failing sda: Disk sda (WDC WD20EFRX) is failing SMART attributes: Reallocated_Sector_Ct.",
				"warning smart.warning sda: Disk sda (WDC WD20EFRX) has SMART warnings: Spin_Retry_Count.",
			},
		},
		{
			"raises an alert for a failed health check with no attribute named",
			func(h *protocol.StorageHealthPayload) { h.Disks[0].Passed, h.Disks[0].Model = false, "" },
			[]string{"critical smart.failing sda: Disk sda is failing its SMART health check."},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			health := healthy()
			tc.change(&health)
			got := describe(alerts.StorageConditions(health, alerts.DefaultCapacityPercent))
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func states(t *testing.T, h *harness) []string {
	t.Helper()
	list, err := h.service.ForAgent(t.Context(), h.agentID, 10)
	if err != nil {
		t.Fatal(err)
	}
	result := []string{}
	for _, alert := range list {
		result = append(result, fmt.Sprintf("%s %s %s", alert.State, alert.Kind, alert.Subject))
	}
	return result
}

func TestRecordStorageHealth(t *testing.T) {
	t.Run("raises an alert when a pool degrades and resolves it when the pool recovers", func(t *testing.T) {
		h := newHarness(t)
		degraded := healthy()
		degraded.Pools[0].Health = "DEGRADED"
		if changed, err := h.service.RecordStorageHealth(t.Context(), h.agentID, degraded); err != nil || !changed {
			t.Fatalf("changed %v, %v", changed, err)
		}
		if got := states(t, h); !slices.Equal(got, []string{"firing zfs.pool.health tank"}) {
			t.Errorf("got %v", got)
		}
		counts, err := h.service.FiringCounts(t.Context())
		if err != nil || counts[h.agentID] != 1 {
			t.Errorf("counts %v, %v", counts, err)
		}

		h.clock.Advance(time.Hour)
		if changed, err := h.service.RecordStorageHealth(t.Context(), h.agentID, healthy()); err != nil || !changed {
			t.Fatalf("changed %v, %v", changed, err)
		}
		list, err := h.service.ForAgent(t.Context(), h.agentID, 10)
		if err != nil || len(list) != 1 || list[0].State != alerts.Resolved || list[0].ResolvedAt != h.clock.Now().UnixMilli() {
			t.Errorf("got %+v, %v", list, err)
		}
		if counts, _ := h.service.FiringCounts(t.Context()); counts[h.agentID] != 0 {
			t.Errorf("counts %v", counts)
		}
	})

	t.Run("keeps one alert for a condition that holds, updating its message", func(t *testing.T) {
		h := newHarness(t)
		full := healthy()
		full.Pools[0].UsedPercent = 85
		if _, err := h.service.RecordStorageHealth(t.Context(), h.agentID, full); err != nil {
			t.Fatal(err)
		}
		if changed, err := h.service.RecordStorageHealth(t.Context(), h.agentID, full); err != nil || changed {
			t.Errorf("an unchanged condition changed something: %v, %v", changed, err)
		}
		full.Pools[0].UsedPercent = 91
		if changed, err := h.service.RecordStorageHealth(t.Context(), h.agentID, full); err != nil || !changed {
			t.Fatalf("changed %v, %v", changed, err)
		}
		list, err := h.service.ForAgent(t.Context(), h.agentID, 10)
		if err != nil || len(list) != 1 || list[0].Message != "Pool tank is 91% full." {
			t.Errorf("got %+v, %v", list, err)
		}
	})

	t.Run("raises a new alert when a resolved condition comes back, keeping the old one as history", func(t *testing.T) {
		h := newHarness(t)
		failing := healthy()
		failing.Disks[0].Passed = false
		for _, health := range []protocol.StorageHealthPayload{failing, healthy(), failing} {
			h.clock.Advance(time.Minute)
			if _, err := h.service.RecordStorageHealth(t.Context(), h.agentID, health); err != nil {
				t.Fatal(err)
			}
		}
		if got := states(t, h); !slices.Equal(got, []string{"firing smart.failing sda", "resolved smart.failing sda"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("lists critical alerts before warnings, and leaves other machines' alone", func(t *testing.T) {
		h := newHarness(t)
		bad := healthy()
		bad.Pools[0].Health, bad.Pools[0].UsedPercent = "FAULTED", 99
		if _, err := h.service.RecordStorageHealth(t.Context(), h.agentID, bad); err != nil {
			t.Fatal(err)
		}
		if got := states(t, h); !slices.Equal(got, []string{"firing zfs.pool.health tank", "firing zfs.pool.capacity tank"}) {
			t.Errorf("got %v", got)
		}
		list, err := h.service.ForAgent(t.Context(), h.agentID+1, 10)
		if err != nil || len(list) != 0 {
			t.Errorf("another machine has %v, %v", list, err)
		}
	})
}

// failingDatabase fails every transaction.
type failingDatabase struct{}

func (failingDatabase) Read(context.Context, func(*sql.Tx) error) error {
	return errors.New("disk I/O error")
}

func (failingDatabase) Write(context.Context, func(*sql.Tx) error) error {
	return errors.New("disk I/O error")
}

func TestDatabaseFailures(t *testing.T) {
	service := alerts.NewService(alerts.Options{Database: failingDatabase{}, Clock: fakeclock.New(time.Now())})
	t.Run("reports a failure to record health", func(t *testing.T) {
		if _, err := service.RecordStorageHealth(t.Context(), 1, healthy()); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("reports a failure to list or count", func(t *testing.T) {
		if _, err := service.ForAgent(t.Context(), 1, 10); err == nil {
			t.Error("expected an error")
		}
		if _, err := service.FiringCounts(t.Context()); err == nil {
			t.Error("expected an error")
		}
		if _, err := service.List(t.Context(), 10); err == nil {
			t.Error("expected an error")
		}
		if _, err := service.Rules(t.Context()); err == nil {
			t.Error("expected an error")
		}
		if _, err := service.Channels(t.Context()); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("reports a failure to evaluate rules or record a scheduled run", func(t *testing.T) {
		if err := service.Evaluate(t.Context()); err == nil {
			t.Error("expected an error")
		}
		if err := service.RecordScheduledRun(t.Context(), 1, 1, "backup", "boom"); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestRecordDockerHealth(t *testing.T) {
	stopped := protocol.DockerHealthPayload{Problems: []protocol.ContainerProblem{
		{Name: "worker", Image: "app:1", State: "exited", ExitCode: 137},
		{Name: "db", Image: "postgres:17", State: "restarting"},
	}}

	t.Run("raises an alert per problem container and resolves each when it recovers", func(t *testing.T) {
		h := newHarness(t)
		if changed, err := h.service.RecordDockerHealth(t.Context(), h.agentID, stopped); err != nil || !changed {
			t.Fatalf("changed %v, %v", changed, err)
		}
		got := states(t, h)
		slices.Sort(got)
		if !slices.Equal(got, []string{"firing container.restarting db", "firing container.stopped worker"}) {
			t.Errorf("got %v", got)
		}
		list, _ := h.service.ForAgent(t.Context(), h.agentID, 10)
		for _, alert := range list {
			if alert.Subject == "worker" && alert.Message != "Container worker (app:1) stopped unexpectedly with exit code 137." {
				t.Errorf("message = %q", alert.Message)
			}
		}
		if changed, err := h.service.RecordDockerHealth(t.Context(), h.agentID, stopped); err != nil || changed {
			t.Errorf("the same problems changed something: %v, %v", changed, err)
		}

		h.clock.Advance(time.Minute)
		recovered := protocol.DockerHealthPayload{Problems: stopped.Problems[1:]}
		if _, err := h.service.RecordDockerHealth(t.Context(), h.agentID, recovered); err != nil {
			t.Fatal(err)
		}
		got = states(t, h)
		slices.Sort(got)
		if !slices.Equal(got, []string{"firing container.restarting db", "resolved container.stopped worker"}) {
			t.Errorf("after worker recovered: %v", got)
		}
	})

	t.Run("raises nothing once the rule is switched off", func(t *testing.T) {
		h := newHarness(t)
		h.exec(t, `UPDATE alert_rules SET enabled = 0 WHERE type = 'container'`)
		if changed, err := h.service.RecordDockerHealth(t.Context(), h.agentID, stopped); err != nil || changed {
			t.Errorf("changed %v, %v", changed, err)
		}
	})

	t.Run("ignores a machine it doesn't know", func(t *testing.T) {
		h := newHarness(t)
		if changed, err := h.service.RecordDockerHealth(t.Context(), 9999, stopped); err != nil || changed {
			t.Errorf("changed %v, %v", changed, err)
		}
	})

	t.Run("reports a database failure", func(t *testing.T) {
		service := alerts.NewService(alerts.Options{Database: failingDatabase{}, Clock: fakeclock.New(time.Now())})
		if _, err := service.RecordDockerHealth(t.Context(), 1, stopped); err == nil {
			t.Error("err = nil")
		}
	})
}

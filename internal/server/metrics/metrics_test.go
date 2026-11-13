// SPDX-License-Identifier: Apache-2.0

package metrics_test

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/metrics"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// harness is a metrics Service on a real database with one agent.
type harness struct {
	service  *metrics.Service
	clock    *fakeclock.Clock
	store    *store.Store
	database *faultyDatabase
	agentID  int64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() }) // nothing to check when a test database closes
	h := &harness{clock: clock, store: database, database: &faultyDatabase{store: database}}
	h.service = metrics.NewService(h.database, clock)
	h.agentID = h.addAgent(t, "nas-01")
	return h
}

func (h *harness) addAgent(t *testing.T, hostname string) int64 {
	t.Helper()
	var id int64
	err := h.store.Write(t.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(t.Context(), `INSERT INTO agents (public_id, hostname, os, arch, agent_version,
			protocol_version, public_key, enrolled_at) VALUES (?, ?, 'linux', 'amd64', 'v1', 1, x'00', 0)`,
			protocol.NewID(h.clock.Now()), hostname)
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// sample is a typical reading taken at the fake clock's current time.
func (h *harness) sample() protocol.MetricsSample {
	return protocol.MetricsSample{
		Time:             h.clock.Now().UnixMilli(),
		CPUPercent:       25,
		MemoryUsedBytes:  2 << 30,
		MemoryTotalBytes: 8 << 30,
		Filesystems: []protocol.FilesystemUsage{
			{Mount: "/", Type: "ext4", UsedBytes: 30, TotalBytes: 100},
			{Mount: "/srv", Type: "zfs", UsedBytes: 90, TotalBytes: 100},
			{Mount: "/empty", Type: "tmpfs", UsedBytes: 0, TotalBytes: 0},
		},
		NetworkReceivedBytes: 1_000_000,
		NetworkSentBytes:     500_000,
		DiskReadBytes:        10_000,
		DiskWrittenBytes:     20_000,
		Temperatures:         []protocol.Temperature{{Sensor: "cpu", Celsius: 55}, {Sensor: "nvme", Celsius: 61.5}},
		UptimeSeconds:        3600,
	}
}

func (h *harness) ingest(t *testing.T, sample protocol.MetricsSample) {
	t.Helper()
	if err := h.service.Ingest(t.Context(), h.agentID, sample); err != nil {
		t.Fatal(err)
	}
}

// query returns the values of a metric over the last span.
func (h *harness) query(t *testing.T, metric string, from, to time.Time) metrics.Series {
	t.Helper()
	series, err := h.service.Query(t.Context(), h.agentID, metric, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	return series
}

func TestIngest(t *testing.T) {
	t.Run("stores CPU, memory, the fullest disk and the hottest sensor", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, h.sample())

		snapshot, ok := h.service.Latest(h.agentID)
		if !ok {
			t.Fatal("no snapshot")
		}
		want := map[string]float64{
			metrics.CPUPercent: 25, metrics.MemoryPercent: 25, metrics.DiskPercent: 90, metrics.Temperature: 61.5,
		}
		for name, value := range want {
			if snapshot.Values[name] != value {
				t.Errorf("%s = %v, want %v", name, snapshot.Values[name], value)
			}
			start := h.clock.Now()
			if points := h.query(t, name, start, start.Add(time.Second)).Points; len(points) != 1 || points[0].Value != value {
				t.Errorf("stored %s = %+v, want %v", name, points, value)
			}
		}
		if _, ok := snapshot.Values[metrics.NetworkReceived]; ok {
			t.Error("the first sample has a rate; there is nothing to compare it with")
		}
	})

	diskCases := []struct {
		name        string
		filesystems []protocol.FilesystemUsage
		pools       []protocol.PoolUsage
		want        float64
	}{
		{
			name: "counts the fullest pool, never a dataset's own use, toward disk %",
			filesystems: []protocol.FilesystemUsage{
				{Mount: "/", Type: "ext4", UsedBytes: 30, TotalBytes: 100},
				{Mount: "/tank/full", Type: "zfs", UsedBytes: 99, TotalBytes: 100},
			},
			pools: []protocol.PoolUsage{{Name: "tank", SizeBytes: 200, AllocatedBytes: 120}},
			want:  60,
		},
		{
			name:        "counts a filesystem fuller than every pool",
			filesystems: []protocol.FilesystemUsage{{Mount: "/", Type: "ext4", UsedBytes: 95, TotalBytes: 100}},
			pools:       []protocol.PoolUsage{{Name: "tank", SizeBytes: 200, AllocatedBytes: 20}},
			want:        95,
		},
		{
			name:        "counts datasets as before for an agent that reports no pools",
			filesystems: []protocol.FilesystemUsage{{Mount: "/tank", Type: "zfs", UsedBytes: 70, TotalBytes: 100}},
			want:        70,
		},
	}
	for _, tc := range diskCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			sample := h.sample()
			sample.Filesystems, sample.Pools = tc.filesystems, tc.pools
			h.ingest(t, sample)
			snapshot, _ := h.service.Latest(h.agentID)
			if got := snapshot.Values[metrics.DiskPercent]; got != tc.want {
				t.Errorf("disk %% = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("turns counters into rates per second", func(t *testing.T) {
		h := newHarness(t)
		first := h.sample()
		h.ingest(t, first)
		h.clock.Advance(10 * time.Second)
		second := h.sample()
		second.NetworkReceivedBytes += 10_000_000
		second.NetworkSentBytes += 1_000
		second.DiskReadBytes += 50_000
		second.DiskWrittenBytes += 0
		h.ingest(t, second)

		snapshot, _ := h.service.Latest(h.agentID)
		want := map[string]float64{
			metrics.NetworkReceived: 1_000_000, metrics.NetworkSent: 100, metrics.DiskRead: 5_000, metrics.DiskWritten: 0,
		}
		for name, value := range want {
			if got, ok := snapshot.Values[name]; !ok || got != value {
				t.Errorf("%s = %v (%v), want %v", name, got, ok, value)
			}
		}
	})

	t.Run("skips rates after a reboot resets the counters", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, h.sample())
		h.clock.Advance(10 * time.Second)
		rebooted := h.sample()
		rebooted.NetworkReceivedBytes = 5
		h.ingest(t, rebooted)

		snapshot, _ := h.service.Latest(h.agentID)
		if _, ok := snapshot.Values[metrics.NetworkReceived]; ok {
			t.Error("a negative rate was turned into a value")
		}
		if _, ok := snapshot.Values[metrics.NetworkSent]; !ok {
			t.Error("counters that didn't reset lost their rate")
		}
	})

	t.Run("an older buffered sample doesn't replace the newest one", func(t *testing.T) {
		h := newHarness(t)
		older := h.sample()
		h.clock.Advance(time.Minute)
		newer := h.sample()
		newer.CPUPercent = 80
		h.ingest(t, newer)
		h.ingest(t, older)

		snapshot, _ := h.service.Latest(h.agentID)
		if snapshot.CPUPercent != 80 {
			t.Errorf("latest CPU = %v, want the newer sample's 80", snapshot.CPUPercent)
		}
		if points := h.query(t, metrics.CPUPercent, h.clock.Now().Add(-time.Hour), h.clock.Now().Add(time.Second)).Points; len(points) != 2 {
			t.Errorf("stored %d points, want both samples", len(points))
		}
	})

	t.Run("keeps the first copy of a resent sample", func(t *testing.T) {
		h := newHarness(t)
		sample := h.sample()
		h.ingest(t, sample)
		sample.CPUPercent = 99
		h.ingest(t, sample)
		points := h.query(t, metrics.CPUPercent, h.clock.Now(), h.clock.Now().Add(time.Second)).Points
		if len(points) != 1 || points[0].Value != 25 {
			t.Errorf("points = %+v, want the first copy only", points)
		}
	})

	t.Run("drops samples from too long ago or too far ahead", func(t *testing.T) {
		h := newHarness(t)
		for _, offset := range []time.Duration{-2 * time.Hour, 10 * time.Minute} {
			sample := h.sample()
			sample.Time = h.clock.Now().Add(offset).UnixMilli()
			h.ingest(t, sample)
		}
		if _, ok := h.service.Latest(h.agentID); ok {
			t.Error("a dropped sample became the latest")
		}
	})

	t.Run("accepts a sample buffered for an hour", func(t *testing.T) {
		h := newHarness(t)
		sample := h.sample()
		sample.Time = h.clock.Now().Add(-time.Hour).UnixMilli()
		h.ingest(t, sample)
		if _, ok := h.service.Latest(h.agentID); !ok {
			t.Error("the buffered sample was dropped")
		}
	})

	t.Run("keeps values sane whatever the agent sends", func(t *testing.T) {
		h := newHarness(t)
		sample := h.sample()
		sample.CPUPercent = math.NaN()
		sample.MemoryUsedBytes = sample.MemoryTotalBytes * 2
		sample.Temperatures = []protocol.Temperature{{Sensor: "bad", Celsius: math.Inf(1)}, {Sensor: "ok", Celsius: -5}}
		h.ingest(t, sample)

		snapshot, _ := h.service.Latest(h.agentID)
		if snapshot.Values[metrics.CPUPercent] != 0 || snapshot.Values[metrics.MemoryPercent] != 100 ||
			snapshot.Values[metrics.Temperature] != -5 {
			t.Errorf("values = %v", snapshot.Values)
		}
	})

	t.Run("drops labels the UI shouldn't show and caps long lists", func(t *testing.T) {
		h := newHarness(t)
		sample := h.sample()
		sample.Filesystems = append(sample.Filesystems,
			protocol.FilesystemUsage{Mount: "/mnt/\u202eevil", Type: "ext4", TotalBytes: 1},
			protocol.FilesystemUsage{Mount: "/" + strings.Repeat("m", 600), Type: "ext4", TotalBytes: 1})
		for range 300 {
			sample.Temperatures = append(sample.Temperatures, protocol.Temperature{Sensor: "core", Celsius: 40})
		}
		h.ingest(t, sample)

		snapshot, _ := h.service.Latest(h.agentID)
		if len(snapshot.Filesystems) != 3 || len(snapshot.Temperatures) != 256 {
			t.Errorf("%d filesystems and %d sensors, want 3 and 256", len(snapshot.Filesystems), len(snapshot.Temperatures))
		}
	})

	t.Run("omits memory, disk and temperature when the machine reports none", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, protocol.MetricsSample{Time: h.clock.Now().UnixMilli(), CPUPercent: 3})
		snapshot, _ := h.service.Latest(h.agentID)
		if len(snapshot.Values) != 1 {
			t.Errorf("values = %v, want CPU only", snapshot.Values)
		}
	})

	t.Run("forgets an agent", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, h.sample())
		h.service.Forget(h.agentID)
		if _, ok := h.service.Latest(h.agentID); ok {
			t.Error("the agent was remembered")
		}
	})
}

func TestRollup(t *testing.T) {
	t.Run("averages samples into minutes and hours, keeping the extremes", func(t *testing.T) {
		h := newHarness(t)
		start := h.clock.Now()
		for i, cpu := range []float64{10, 20, 30, 40, 50, 60} { // one minute of samples
			sample := h.sample()
			sample.Time = start.Add(time.Duration(i) * 10 * time.Second).UnixMilli()
			sample.CPUPercent = cpu
			h.ingest(t, sample)
		}
		sample := h.sample()
		sample.Time = start.Add(time.Minute).UnixMilli()
		sample.CPUPercent = 100
		h.ingest(t, sample)
		h.clock.Advance(2 * time.Minute)
		if err := h.service.Rollup(t.Context()); err != nil {
			t.Fatal(err)
		}

		// A 3-day range is answered from the hourly table.
		hours := h.query(t, metrics.CPUPercent, start.Add(-72*time.Hour), start.Add(time.Hour))
		if hours.Resolution != metrics.ResolutionHour || len(hours.Points) != 1 {
			t.Fatalf("hours = %+v", hours)
		}
		// The hour averages its two minutes (35 and 100), not its 7 samples.
		if got := hours.Points[0]; got.Value != 67.5 || got.Min != 10 || got.Max != 100 {
			t.Errorf("hour = %+v, want average 67.5, min 10, max 100", got)
		}
		minutes := h.query(t, metrics.CPUPercent, start.Add(-24*time.Hour), start.Add(time.Hour))
		if minutes.Resolution != metrics.ResolutionMinute || len(minutes.Points) != 2 || minutes.Points[0].Value != 35 {
			t.Errorf("minutes = %+v", minutes)
		}
	})

	t.Run("recomputes a bucket when a late sample arrives", func(t *testing.T) {
		h := newHarness(t)
		start := h.clock.Now()
		h.ingest(t, h.sample())
		h.clock.Advance(time.Minute)
		if err := h.service.Rollup(t.Context()); err != nil {
			t.Fatal(err)
		}
		late := h.sample()
		late.Time = start.Add(30 * time.Second).UnixMilli()
		late.CPUPercent = 75
		h.ingest(t, late)
		if err := h.service.Rollup(t.Context()); err != nil {
			t.Fatal(err)
		}
		minutes := h.query(t, metrics.CPUPercent, start.Add(-24*time.Hour), start.Add(time.Hour))
		if len(minutes.Points) != 1 || minutes.Points[0].Value != 50 {
			t.Errorf("minutes = %+v, want one minute averaging 25 and 75", minutes.Points)
		}
	})

	t.Run("does nothing when no samples arrived", func(t *testing.T) {
		h := newHarness(t)
		if err := h.service.Rollup(t.Context()); err != nil {
			t.Fatal(err)
		}
		h.database.failWrites.Store(true)
		if err := h.service.Rollup(t.Context()); err != nil {
			t.Errorf("second Rollup = %v, want nothing to do", err)
		}
	})

	t.Run("retries the same buckets after a failure", func(t *testing.T) {
		h := newHarness(t)
		start := h.clock.Now()
		h.ingest(t, h.sample())
		h.database.failWrites.Store(true)
		if err := h.service.Rollup(t.Context()); !errors.Is(err, errInjected) {
			t.Fatalf("Rollup = %v, want the injected failure", err)
		}
		h.database.failWrites.Store(false)
		if err := h.service.Rollup(t.Context()); err != nil {
			t.Fatal(err)
		}
		if minutes := h.query(t, metrics.CPUPercent, start.Add(-24*time.Hour), start.Add(time.Hour)); len(minutes.Points) != 1 {
			t.Errorf("minutes = %+v, want the retried bucket", minutes.Points)
		}
	})

	for _, table := range []string{"metrics_1m", "metrics_1h"} {
		t.Run("reports a failure writing "+table, func(t *testing.T) {
			h := newHarness(t)
			h.ingest(t, h.sample())
			h.failInsert(t, table)
			if err := h.service.Rollup(t.Context()); err == nil {
				t.Error("Rollup returned nil")
			}
		})
	}
}

func TestPrune(t *testing.T) {
	t.Run("deletes each table's rows past its retention", func(t *testing.T) {
		h := newHarness(t)
		start := h.clock.Now()
		h.ingest(t, h.sample())
		h.clock.Advance(2 * time.Minute)
		if err := h.service.Rollup(t.Context()); err != nil {
			t.Fatal(err)
		}

		h.clock.Set(start.Add(metrics.RawRetention + time.Minute))
		if err := h.service.Prune(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := h.count(t, "metrics_raw"); got != 0 {
			t.Errorf("%d raw rows after a day, want 0", got)
		}
		if got := h.count(t, "metrics_1m"); got == 0 {
			t.Error("minute rows deleted after only a day")
		}

		h.clock.Set(start.Add(metrics.MinuteRetention + time.Hour))
		if err := h.service.Prune(t.Context()); err != nil {
			t.Fatal(err)
		}
		if h.count(t, "metrics_1m") != 0 || h.count(t, "metrics_1h") == 0 {
			t.Error("after 14 days, want minutes gone and hours kept")
		}

		h.clock.Set(start.Add(metrics.HourRetention + time.Hour))
		if err := h.service.Prune(t.Context()); err != nil {
			t.Fatal(err)
		}
		if h.count(t, "metrics_1h") != 0 {
			t.Error("hour rows kept for over a year")
		}
	})

	t.Run("keeps raw samples as long as the Settings page says", func(t *testing.T) {
		h := newHarness(t)
		start := h.clock.Now()
		h.ingest(t, h.sample())
		h.service.SetRetention(metrics.Retention{Raw: 72 * time.Hour, Minute: metrics.MinuteRetention, Hour: metrics.HourRetention})
		h.clock.Set(start.Add(48 * time.Hour))
		if err := h.service.Prune(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := h.count(t, "metrics_raw"); got == 0 {
			t.Error("raw rows deleted after 2 days with 3 days' retention")
		}
	})

	t.Run("reports a database failure", func(t *testing.T) {
		h := newHarness(t)
		h.database.failWrites.Store(true)
		if err := h.service.Prune(t.Context()); err == nil {
			t.Error("Prune returned nil")
		}
	})

	t.Run("reports a failure deleting rows", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, h.sample())
		h.clock.Advance(48 * time.Hour)
		h.failDelete(t, "metrics_raw")
		if err := h.service.Prune(t.Context()); err == nil {
			t.Error("Prune returned nil")
		}
	})
}

func TestQuery(t *testing.T) {
	t.Run("uses raw samples for recent short ranges", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, h.sample())
		series := h.query(t, metrics.CPUPercent, h.clock.Now().Add(-time.Hour), h.clock.Now().Add(time.Second))
		if series.Resolution != metrics.ResolutionRaw || len(series.Points) != 1 {
			t.Errorf("series = %+v", series)
		}
		if point := series.Points[0]; point.Min != point.Value || point.Max != point.Value {
			t.Errorf("raw point = %+v, want min and max equal to the value", point)
		}
	})

	t.Run("uses minutes for a short range older than the raw table keeps", func(t *testing.T) {
		h := newHarness(t)
		now := h.clock.Now()
		series := h.query(t, metrics.CPUPercent, now.Add(-30*time.Hour), now.Add(-29*time.Hour))
		if series.Resolution != metrics.ResolutionMinute {
			t.Errorf("resolution = %s, want 1m", series.Resolution)
		}
	})

	t.Run("uses hours for a short range older than the minute table keeps", func(t *testing.T) {
		h := newHarness(t)
		now := h.clock.Now()
		series := h.query(t, metrics.CPUPercent, now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour+time.Hour))
		if series.Resolution != metrics.ResolutionHour || series.Points == nil {
			t.Errorf("series = %+v, want an empty hourly series", series)
		}
	})

	t.Run("refuses an unknown metric and a backwards range", func(t *testing.T) {
		h := newHarness(t)
		now := h.clock.Now().UnixMilli()
		var validation *auth.ValidationError
		if _, err := h.service.Query(t.Context(), h.agentID, "cpu.percent; DROP TABLE agents", now-1, now); !errors.As(err, &validation) {
			t.Errorf("unknown metric: err = %v", err)
		}
		if _, err := h.service.Query(t.Context(), h.agentID, metrics.CPUPercent, now, now); !errors.As(err, &validation) {
			t.Errorf("empty range: err = %v", err)
		}
	})

	t.Run("reports a database failure", func(t *testing.T) {
		h := newHarness(t)
		h.database.failReads.Store(true)
		if _, err := h.service.Query(t.Context(), h.agentID, metrics.CPUPercent, 0, 1); err == nil {
			t.Error("Query returned nil")
		}
	})

	t.Run("keeps agents' metrics apart", func(t *testing.T) {
		h := newHarness(t)
		other := h.addAgent(t, "web-01")
		if err := h.service.Ingest(t.Context(), other, h.sample()); err != nil {
			t.Fatal(err)
		}
		if points := h.query(t, metrics.CPUPercent, h.clock.Now(), h.clock.Now().Add(time.Second)).Points; len(points) != 0 {
			t.Errorf("agent sees another agent's points: %+v", points)
		}
	})
}

func TestNames(t *testing.T) {
	t.Run("knows every listed metric and nothing else", func(t *testing.T) {
		for _, name := range metrics.Names() {
			if !metrics.IsName(name) {
				t.Errorf("IsName(%q) = false", name)
			}
		}
		if metrics.IsName("cpu") {
			t.Error(`IsName("cpu") = true`)
		}
	})
}

func TestIngestFailure(t *testing.T) {
	t.Run("reports a database failure", func(t *testing.T) {
		h := newHarness(t)
		h.database.failWrites.Store(true)
		if err := h.service.Ingest(t.Context(), h.agentID, h.sample()); !errors.Is(err, errInjected) {
			t.Errorf("err = %v, want the injected failure", err)
		}
	})

	t.Run("reports a failure storing a metric", func(t *testing.T) {
		h := newHarness(t)
		h.failInsert(t, "metrics_raw")
		if err := h.service.Ingest(t.Context(), h.agentID, h.sample()); err == nil {
			t.Error("Ingest returned nil")
		}
	})
}

func (h *harness) count(t *testing.T, table string) int {
	t.Helper()
	var count int
	err := h.store.Read(t.Context(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// failInsert makes inserts into table fail, including the rollups' INSERT
// OR REPLACE.
func (h *harness) failInsert(t *testing.T, table string) {
	t.Helper()
	h.addTrigger(t, "BEFORE INSERT ON "+table)
}

func (h *harness) failDelete(t *testing.T, table string) {
	t.Helper()
	h.addTrigger(t, "BEFORE DELETE ON "+table)
}

func (h *harness) addTrigger(t *testing.T, when string) {
	t.Helper()
	err := h.store.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `CREATE TRIGGER injected `+when+` BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// errInjected is what faultyDatabase returns when told to fail.
var errInjected = errors.New("injected database failure")

type faultyDatabase struct {
	store      *store.Store
	failReads  atomic.Bool
	failWrites atomic.Bool
}

func (d *faultyDatabase) Read(ctx context.Context, work func(tx *sql.Tx) error) error {
	if d.failReads.Load() {
		return errInjected
	}
	return d.store.Read(ctx, work)
}

func (d *faultyDatabase) Write(ctx context.Context, work func(tx *sql.Tx) error) error {
	if d.failWrites.Load() {
		return errInjected
	}
	return d.store.Write(ctx, work)
}

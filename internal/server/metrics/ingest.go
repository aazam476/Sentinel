// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Limits on samples. An agent buffers at most an hour while disconnected;
// a little more is allowed for clock drift between machines.
const (
	maxSampleAge    = time.Hour + 5*time.Minute
	maxSampleFuture = 5 * time.Minute
	// maxListed caps the filesystems and sensors kept from one sample.
	maxListed = 256
	// maxLabelLength caps mount points, filesystem types and sensor names.
	maxLabelLength = 512
)

// Ingest stores one sample from an agent. A sample from too far in the
// past or future is dropped without an error: it would only distort the
// charts, and the agent can't do anything better with it.
func (s *Service) Ingest(ctx context.Context, agentID int64, sample protocol.MetricsSample) error {
	now := s.clock.Now()
	if sample.Time < now.Add(-maxSampleAge).UnixMilli() || sample.Time > now.Add(maxSampleFuture).UnixMilli() {
		return nil
	}
	sample = cleanSample(sample)

	s.mutex.Lock()
	snapshot := s.remember(agentID, sample)
	if s.dirtySince == 0 || sample.Time < s.dirtySince {
		s.dirtySince = sample.Time
	}
	s.mutex.Unlock()

	return s.database.Write(ctx, func(tx *sql.Tx) error {
		for name, value := range snapshot.Values {
			// OR IGNORE: an agent resends buffered samples it isn't sure
			// were stored; the first copy wins.
			_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO metrics_raw (agent_id, metric, ts, value) VALUES (?, ?, ?, ?)`,
				agentID, name, sample.Time, value)
			if err != nil {
				return fmt.Errorf("store metric %s: %w", name, err)
			}
		}
		return nil
	})
}

// remember works out the sample's metrics, updates the agent's state and
// returns the snapshot. The caller holds s.mutex.
func (s *Service) remember(agentID int64, sample protocol.MetricsSample) Snapshot {
	state, ok := s.agents[agentID]
	if !ok {
		state = &agentState{}
		s.agents[agentID] = state
	}
	snapshot := Snapshot{MetricsSample: sample, Values: gauges(sample)}
	next := countersOf(sample)
	addRates(snapshot.Values, state.counters, next)
	// Buffered samples can arrive after newer ones from a previous
	// connection; only a newer sample replaces the snapshot and counters.
	if sample.Time >= state.counters.time {
		state.latest, state.counters = snapshot, next
	}
	return snapshot
}

// fullestStorage is the use of the fullest pool or filesystem (§9.5).
// ZFS datasets are left out: a dataset's own used and free space says
// little, since it shares its pool's space. Agents before M5 sent datasets
// as filesystems and no pools; for them, datasets count as before.
func fullestStorage(sample protocol.MetricsSample) (float64, bool) {
	fullest, found := 0.0, false
	for _, filesystem := range sample.Filesystems {
		if filesystem.TotalBytes > 0 && (filesystem.Type != "zfs" || len(sample.Pools) == 0) {
			fullest, found = math.Max(fullest, percent(filesystem.UsedBytes, filesystem.TotalBytes)), true
		}
	}
	for _, pool := range sample.Pools {
		if pool.SizeBytes > 0 {
			fullest, found = math.Max(fullest, percent(pool.AllocatedBytes, pool.SizeBytes)), true
		}
	}
	return fullest, found
}

// gauges are the metrics read straight off a sample.
func gauges(sample protocol.MetricsSample) map[string]float64 {
	values := map[string]float64{CPUPercent: clampPercent(sample.CPUPercent)}
	if sample.MemoryTotalBytes > 0 {
		values[MemoryPercent] = percent(sample.MemoryUsedBytes, sample.MemoryTotalBytes)
	}
	if fullest, found := fullestStorage(sample); found {
		values[DiskPercent] = fullest
	}
	hottest, found := 0.0, false
	for _, temperature := range sample.Temperatures {
		if !found || temperature.Celsius > hottest {
			hottest, found = temperature.Celsius, true
		}
	}
	if found {
		values[Temperature] = hottest
	}
	return values
}

func countersOf(sample protocol.MetricsSample) counters {
	return counters{
		time: sample.Time, received: sample.NetworkReceivedBytes, sent: sample.NetworkSentBytes,
		diskRead: sample.DiskReadBytes, diskWritten: sample.DiskWrittenBytes,
	}
}

// addRates adds bytes-per-second rates between two consecutive samples.
// There are none for the first sample, for samples out of order, or after
// a counter went backwards (the machine rebooted).
func addRates(values map[string]float64, previous, next counters) {
	if previous.time == 0 || next.time <= previous.time {
		return
	}
	seconds := float64(next.time-previous.time) / 1000
	pairs := []struct {
		name          string
		before, after uint64
	}{
		{NetworkReceived, previous.received, next.received},
		{NetworkSent, previous.sent, next.sent},
		{DiskRead, previous.diskRead, next.diskRead},
		{DiskWritten, previous.diskWritten, next.diskWritten},
	}
	for _, pair := range pairs {
		if pair.after >= pair.before {
			values[pair.name] = float64(pair.after-pair.before) / seconds
		}
	}
}

func percent(used, total uint64) float64 {
	return clampPercent(float64(used) / float64(total) * 100)
}

// clampPercent keeps a percentage between 0 and 100; NaN becomes 0.
func clampPercent(value float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	return math.Min(100, math.Max(0, value))
}

// cleanSample drops what the UI shouldn't display: lists beyond a sane
// size, labels with hidden control characters or absurd lengths, and
// temperatures that aren't numbers.
func cleanSample(sample protocol.MetricsSample) protocol.MetricsSample {
	filesystems := make([]protocol.FilesystemUsage, 0, min(len(sample.Filesystems), maxListed))
	for _, filesystem := range sample.Filesystems {
		if len(filesystems) < maxListed && isCleanLabel(filesystem.Mount) && isCleanLabel(filesystem.Type) {
			filesystems = append(filesystems, filesystem)
		}
	}
	temperatures := make([]protocol.Temperature, 0, min(len(sample.Temperatures), maxListed))
	for _, temperature := range sample.Temperatures {
		finite := !math.IsNaN(temperature.Celsius) && !math.IsInf(temperature.Celsius, 0)
		if len(temperatures) < maxListed && finite && isCleanLabel(temperature.Sensor) {
			temperatures = append(temperatures, temperature)
		}
	}
	sample.Filesystems, sample.Temperatures = filesystems, temperatures
	return sample
}

func isCleanLabel(label string) bool {
	return len(label) <= maxLabelLength && !auth.HasHiddenControls(label)
}

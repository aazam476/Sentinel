// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Rollup recomputes the per-minute and per-hour rows for every bucket that
// received samples since the last run. Recomputing whole buckets, instead
// of adding to them, keeps late samples correct and makes running it twice
// harmless.
func (s *Service) Rollup(ctx context.Context) error {
	s.mutex.Lock()
	since := s.dirtySince
	s.dirtySince = 0
	s.mutex.Unlock()
	if since == 0 {
		return nil
	}

	minuteStart := floor(since, time.Minute)
	hourStart := floor(since, time.Hour)
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		// Minutes from the raw samples...
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO metrics_1m (agent_id, metric, ts, value, min, max)
			SELECT agent_id, metric, ts - ts % 60000, avg(value), min(value), max(value)
			FROM metrics_raw WHERE ts >= ? GROUP BY agent_id, metric, ts - ts % 60000`, minuteStart)
		if err != nil {
			return fmt.Errorf("roll up minutes: %w", err)
		}
		// ...then hours from the minutes. Each minute counts the same,
		// however many samples it had.
		_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO metrics_1h (agent_id, metric, ts, value, min, max)
			SELECT agent_id, metric, ts - ts % 3600000, avg(value), min(min), max(max)
			FROM metrics_1m WHERE ts >= ? GROUP BY agent_id, metric, ts - ts % 3600000`, hourStart)
		if err != nil {
			return fmt.Errorf("roll up hours: %w", err)
		}
		return nil
	})
	if err != nil {
		s.markDirty(since) // try these buckets again next time
	}
	return err
}

func (s *Service) markDirty(since int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.dirtySince == 0 || since < s.dirtySince {
		s.dirtySince = since
	}
}

// Prune deletes rows older than each table's retention.
func (s *Service) Prune(ctx context.Context) error {
	now := s.clock.Now()
	retention := s.currentRetention()
	deletions := []struct {
		statement string
		retention time.Duration
	}{
		{`DELETE FROM metrics_raw WHERE ts < ?`, retention.Raw},
		{`DELETE FROM metrics_1m WHERE ts < ?`, retention.Minute},
		{`DELETE FROM metrics_1h WHERE ts < ?`, retention.Hour},
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		for _, deletion := range deletions {
			if _, err := tx.ExecContext(ctx, deletion.statement, now.Add(-deletion.retention).UnixMilli()); err != nil {
				return fmt.Errorf("prune old metrics: %w", err)
			}
		}
		return nil
	})
}

// floor rounds a Unix millisecond time down to a whole bucket.
func floor(milliseconds int64, bucket time.Duration) int64 {
	return milliseconds - milliseconds%bucket.Milliseconds()
}

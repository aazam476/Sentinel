// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// Resolution is how far apart a series' points are.
type Resolution string

// The resolutions, one per table.
const (
	ResolutionRaw    Resolution = "raw"
	ResolutionMinute Resolution = "1m"
	ResolutionHour   Resolution = "1h"
)

// Point is one point of a series. For raw samples Min and Max equal Value.
type Point struct {
	Time  int64
	Value float64
	Min   float64
	Max   float64
}

// Series is a metric of an agent over a time range.
type Series struct {
	Resolution Resolution
	Points     []Point
}

// Which table answers a query: the finest one that still holds the start
// of the range and gives a chart at most a few thousand points.
const (
	maxRawSpan    = 4 * time.Hour      // 1,440 points
	maxMinuteSpan = 2 * 24 * time.Hour // 2,880 points
)

// Query returns one metric of one agent from from to to (Unix
// milliseconds, to exclusive). The caller checks the user may see the agent.
func (s *Service) Query(ctx context.Context, agentID int64, metric string, from, to int64) (Series, error) {
	if !IsName(metric) {
		return Series{}, &auth.ValidationError{Field: "metric", Message: "There is no metric with this name."}
	}
	if to <= from {
		return Series{}, &auth.ValidationError{Field: "from", Message: "The start must be before the end."}
	}
	resolution := s.resolutionFor(from, to)
	series := Series{Resolution: resolution, Points: []Point{}}
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, queryFor(resolution), agentID, metric, from, to)
		if err != nil {
			return fmt.Errorf("query metric %s: %w", metric, err)
		}
		defer func() { _ = rows.Close() }() // read-only; nothing to report
		for rows.Next() {
			var point Point
			if err := rows.Scan(&point.Time, &point.Value, &point.Min, &point.Max); err != nil {
				return fmt.Errorf("read metric %s: %w", metric, err)
			}
			series.Points = append(series.Points, point)
		}
		return rows.Err()
	})
	return series, err
}

func (s *Service) resolutionFor(from, to int64) Resolution {
	now := s.clock.Now()
	retention := s.currentRetention()
	span := time.Duration(to-from) * time.Millisecond
	switch {
	case span <= maxRawSpan && from >= now.Add(-retention.Raw).UnixMilli():
		return ResolutionRaw
	case span <= maxMinuteSpan && from >= now.Add(-retention.Minute).UnixMilli():
		return ResolutionMinute
	default:
		return ResolutionHour
	}
}

func queryFor(resolution Resolution) string {
	switch resolution {
	case ResolutionRaw:
		return `SELECT ts, value, value, value FROM metrics_raw
			WHERE agent_id = ? AND metric = ? AND ts >= ? AND ts < ? ORDER BY ts`
	case ResolutionMinute:
		return `SELECT ts, value, min, max FROM metrics_1m
			WHERE agent_id = ? AND metric = ? AND ts >= ? AND ts < ? ORDER BY ts`
	default:
		return `SELECT ts, value, min, max FROM metrics_1h
			WHERE agent_id = ? AND metric = ? AND ts >= ? AND ts < ? ORDER BY ts`
	}
}

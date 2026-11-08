// SPDX-License-Identifier: Apache-2.0

// Package metrics stores the samples agents send every 10 seconds and
// answers the charts' questions about them.
//
// Each sample becomes a handful of named metrics (see names.go). Network
// and disk counters arrive as totals since boot; Ingest turns them into
// rates by comparing each sample with the agent's previous one.
//
// Three tables keep the history (§9.1):
//
//   - metrics_raw: every sample, kept 24 hours
//   - metrics_1m: per-minute average, minimum and maximum, kept 14 days
//   - metrics_1h: per-hour average, minimum and maximum, kept 1 year
//
// Rollup, run every minute by the scheduler, recomputes the minutes and
// hours that received samples since its last run. Samples can arrive late
// (an agent buffers up to an hour while disconnected), so it recomputes
// whole buckets rather than adding to them. Prune, run every hour, deletes
// rows past their retention.
//
// The newest sample of each agent is also kept in memory, for the fleet
// view's current CPU, memory and disk figures.
package metrics

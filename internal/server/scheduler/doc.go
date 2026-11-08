// SPDX-License-Identifier: Apache-2.0

// Package scheduler runs the server's periodic maintenance jobs, such as
// metric rollups every minute and retention cleanup every hour.
//
// Each job runs in its own goroutine, waits its interval, runs, and waits
// again; a slow run delays that job's next run rather than overlapping it.
// A failed run is logged and the job carries on. Scheduled scripts and
// Docker checks join this package in a later milestone.
package scheduler

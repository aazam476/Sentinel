// SPDX-License-Identifier: Apache-2.0

// Package scripts keeps the script library (§9.8), runs scripts on
// machines (one, a tag, or a hand-picked set), collects each machine's
// output and exit code, and runs scripts on cron schedules.
//
// A run's output arrives on a stream from the agent as the script writes
// it. While a run is going, its output is kept in memory for the UI to
// follow; when it ends, the output (up to 1 MB) is stored with the run.
package scripts

// SPDX-License-Identifier: Apache-2.0

// Package logs reads the machine's system log (§9.6): journald through
// journalctl on Linux, the unified log through `log show` on macOS, and the
// Event Log through wevtutil on Windows. Entries come back newest first,
// with severities folded into the five levels the UI shows.
package logs

// SPDX-License-Identifier: Apache-2.0

package metrics

import "slices"

// The metrics stored for every agent. The names are stored in the database
// and used by the API, so they never change.
const (
	// CPUPercent is overall CPU use, 0 to 100.
	CPUPercent = "cpu.percent"
	// MemoryPercent is RAM in use, 0 to 100.
	MemoryPercent = "memory.percent"
	// DiskPercent is the fullest filesystem's use, 0 to 100.
	DiskPercent = "disk.percent"
	// NetworkReceived and NetworkSent are bytes per second, all interfaces.
	NetworkReceived = "network.received"
	NetworkSent     = "network.sent"
	// DiskRead and DiskWritten are bytes per second, all disks.
	DiskRead    = "disk.read"
	DiskWritten = "disk.written"
	// Temperature is the hottest sensor, in degrees Celsius. Machines
	// without sensors have none.
	Temperature = "temperature.max"
)

// Names lists every metric, in the order the UI shows them.
func Names() []string {
	return []string{CPUPercent, MemoryPercent, DiskPercent, NetworkReceived, NetworkSent, DiskRead, DiskWritten, Temperature}
}

// IsName reports whether name is one of the metrics.
func IsName(name string) bool {
	return slices.Contains(Names(), name)
}

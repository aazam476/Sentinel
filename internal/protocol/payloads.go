// SPDX-License-Identifier: Apache-2.0

package protocol

// Action names used so far. Later features add theirs here.
const (
	// ActionMetricsSample is the event an agent sends every 10 seconds.
	ActionMetricsSample = "metrics.sample"
	// ActionSystemInfo asks an agent to describe its machine.
	ActionSystemInfo = "system.info"
)

// MetricsSample is one reading of a machine's vital signs, taken by the
// agent. Counters (network, disk I/O) are totals since boot; the server
// turns them into rates.
type MetricsSample struct {
	// Time is when the sample was taken, UTC Unix milliseconds.
	Time int64 `json:"time"`
	// CPUPercent is overall CPU use, 0 to 100.
	CPUPercent float64 `json:"cpuPercent"`
	// MemoryUsedBytes and MemoryTotalBytes describe RAM.
	MemoryUsedBytes  uint64 `json:"memoryUsedBytes"`
	MemoryTotalBytes uint64 `json:"memoryTotalBytes"`
	// Filesystems lists mounted filesystems with their usage.
	Filesystems []FilesystemUsage `json:"filesystems"`
	// Network and disk counters since boot.
	NetworkReceivedBytes uint64 `json:"networkReceivedBytes"`
	NetworkSentBytes     uint64 `json:"networkSentBytes"`
	DiskReadBytes        uint64 `json:"diskReadBytes"`
	DiskWrittenBytes     uint64 `json:"diskWrittenBytes"`
	// Temperatures is empty where the machine reports none.
	Temperatures []Temperature `json:"temperatures"`
	// UptimeSeconds is how long the machine has been running.
	UptimeSeconds uint64 `json:"uptimeSeconds"`
}

// FilesystemUsage is one mounted filesystem.
type FilesystemUsage struct {
	Mount      string `json:"mount"`
	Type       string `json:"type"`
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

// Temperature is one sensor reading.
type Temperature struct {
	Sensor  string  `json:"sensor"`
	Celsius float64 `json:"celsius"`
}

// SystemInfo describes a machine and what its agent can do.
type SystemInfo struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platformVersion"`
	KernelVersion   string `json:"kernelVersion"`
	Arch            string `json:"arch"`
	CPUModel        string `json:"cpuModel"`
	CPUCores        int    `json:"cpuCores"`
	MemoryTotal     uint64 `json:"memoryTotalBytes"`
	AgentVersion    string `json:"agentVersion"`
	// Capabilities says which features the local policy allows.
	Capabilities []Capability `json:"capabilities"`
}

// Capability is one feature and whether this machine allows it.
type Capability struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Reason explains a disabled capability, e.g. "Disabled by local policy".
	Reason string `json:"reason,omitempty"`
}

// StreamClosePayload ends a stream.
type StreamClosePayload struct {
	StreamID StreamID `json:"streamId"`
	Reason   string   `json:"reason"`
}

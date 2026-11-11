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
	// Filesystems lists mounted filesystems with their usage, leaving out
	// ZFS datasets and pseudo filesystems.
	Filesystems []FilesystemUsage `json:"filesystems"`
	// Pools lists ZFS pools with their usage; absent without ZFS.
	Pools []PoolUsage `json:"pools,omitempty"`
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
	// Desktop says whether remote desktop can be used now; absent from
	// agents that predate it.
	Desktop *DesktopAvailability `json:"desktop,omitempty"`
}

// DesktopAvailability says whether a machine's desktop can be shared.
type DesktopAvailability struct {
	Available bool `json:"available"`
	// Reason says why not, e.g. "No one is logged in to a graphical
	// session".
	Reason string `json:"reason,omitempty"`
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
	// KeepSession, from the server, means the viewer went away rather than
	// closed it: the agent keeps a terminal's shell running for 30 seconds
	// so the browser can reattach.
	KeepSession bool `json:"keepSession,omitempty"`
}

// Terminal actions (§9.2). The server picks the stream ID and sends it in
// terminal.open, so it is ready for the terminal's output before the agent
// can send any.
const (
	ActionTerminalOpen   = "terminal.open"
	ActionTerminalResize = "terminal.resize"
)

// TerminalOpenPayload asks the agent to start a shell, or to reattach to
// one whose browser connection dropped less than 30 seconds ago.
type TerminalOpenPayload struct {
	StreamID StreamID `json:"streamId"`
	Cols     uint16   `json:"cols"`
	Rows     uint16   `json:"rows"`
	// ResumeSessionID reattaches to that session instead of starting one.
	ResumeSessionID string `json:"resumeSessionId,omitempty"`
	// User is who opened it, for the tray on the machine (ADR 0038).
	User string `json:"user,omitempty"`
}

// TerminalOpenedPayload answers terminal.open.
type TerminalOpenedPayload struct {
	// SessionID identifies the shell, for reattaching after a drop.
	SessionID string `json:"sessionId"`
	// Resumed is true when an existing shell was reattached; its recent
	// output is sent again first so the screen can be redrawn.
	Resumed bool `json:"resumed"`
}

// TerminalResizePayload changes a terminal's size.
type TerminalResizePayload struct {
	StreamID StreamID `json:"streamId"`
	Cols     uint16   `json:"cols"`
	Rows     uint16   `json:"rows"`
}

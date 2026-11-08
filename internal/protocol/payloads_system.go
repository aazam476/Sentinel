// SPDX-License-Identifier: Apache-2.0

package protocol

// System management actions (§9.6): services, processes, logs and power.
const (
	ActionServiceList    = "service.list"
	ActionServiceStart   = "service.start"
	ActionServiceStop    = "service.stop"
	ActionServiceRestart = "service.restart"
	ActionServiceEnable  = "service.enable"
	ActionServiceDisable = "service.disable"
	ActionServiceLogs    = "service.logs"

	ActionProcessList = "process.list"
	ActionProcessKill = "process.kill"

	ActionLogQuery = "log.query"

	ActionSystemReboot   = "system.reboot"
	ActionSystemShutdown = "system.shutdown"
)

// ServiceState is whether a service is running.
type ServiceState string

// The states the UI shows. Each service manager's own states map onto these.
const (
	ServiceRunning  ServiceState = "running"
	ServiceStopped  ServiceState = "stopped"
	ServiceFailed   ServiceState = "failed"
	ServiceStarting ServiceState = "starting"
	ServiceStopping ServiceState = "stopping"
	ServiceUnknown  ServiceState = "unknown"
)

// ServiceStartup is whether a service starts when the machine boots.
type ServiceStartup string

// The startup modes the UI shows.
const (
	// StartupAutomatic: starts at boot (systemd "enabled", launchd not
	// disabled, Windows "Automatic").
	StartupAutomatic ServiceStartup = "automatic"
	// StartupManual: only when started by hand or by another service.
	StartupManual ServiceStartup = "manual"
	// StartupDisabled: can't be started until enabled again.
	StartupDisabled ServiceStartup = "disabled"
	// StartupFixed: the service manager decides, and enable/disable don't
	// apply (systemd "static", "alias" and similar).
	StartupFixed ServiceStartup = "fixed"
)

// Service is one service on the machine.
type Service struct {
	// Name is what the service manager calls it: a systemd unit, a launchd
	// label or a Windows service name. Actions name services by it.
	Name string `json:"name"`
	// Description is a human-readable name, or empty.
	Description string         `json:"description"`
	State       ServiceState   `json:"state"`
	Startup     ServiceStartup `json:"startup"`
	// PID is the main process, or 0 when the service isn't running.
	PID int `json:"pid"`
}

// ServiceListPayload answers service.list.
type ServiceListPayload struct {
	// Manager names the service manager: "systemd", "launchd" or "scm".
	Manager  string    `json:"manager"`
	Services []Service `json:"services"`
}

// ServiceActionPayload names the service for service.start, stop, restart,
// enable and disable.
type ServiceActionPayload struct {
	Name string `json:"name"`
}

// Process is one running process.
type Process struct {
	PID  int32  `json:"pid"`
	Name string `json:"name"`
	// User is the account it runs as, or empty if unknown.
	User string `json:"user"`
	// CPUPercent is its CPU use over the last second, where 100 is one
	// whole core (so it can exceed 100 on a multi-core machine).
	CPUPercent  float64 `json:"cpuPercent"`
	MemoryBytes uint64  `json:"memoryBytes"`
	// Command is the full command line, or empty if the agent can't read it.
	Command string `json:"command"`
}

// ProcessListPayload answers process.list.
type ProcessListPayload struct {
	Processes []Process `json:"processes"`
}

// ProcessKillPayload asks the agent to end a process.
type ProcessKillPayload struct {
	PID int32 `json:"pid"`
	// Force kills it outright (SIGKILL) instead of asking it to stop
	// (SIGTERM). On Windows every kill is forced.
	Force bool `json:"force"`
}

// LogLevel is a log entry's severity, from syslog's eight levels folded
// into five.
type LogLevel string

// The log levels, from most to least severe.
const (
	LogError   LogLevel = "error"
	LogWarning LogLevel = "warning"
	LogNotice  LogLevel = "notice"
	LogInfo    LogLevel = "info"
	LogDebug   LogLevel = "debug"
)

// LogQueryPayload asks for recent log entries, newest first. service.logs
// takes the same payload with Service set.
type LogQueryPayload struct {
	// Service limits the entries to one service, by its Name.
	Service string `json:"service,omitempty"`
	// Since is the oldest entry wanted, UTC Unix milliseconds; 0 means the
	// last hour.
	Since int64 `json:"since,omitempty"`
	// MinLevel drops entries less severe than this; empty means all.
	MinLevel LogLevel `json:"minLevel,omitempty"`
	// Limit is the most entries wanted: 1 to 1,000, default 200.
	Limit int `json:"limit,omitempty"`
}

// LogEntry is one log line.
type LogEntry struct {
	// Time is UTC Unix milliseconds.
	Time  int64    `json:"time"`
	Level LogLevel `json:"level"`
	// Source is what logged it: a process, unit or event provider.
	Source  string `json:"source"`
	Message string `json:"message"`
}

// LogEntriesPayload answers log.query and service.logs.
type LogEntriesPayload struct {
	Entries []LogEntry `json:"entries"`
	// Truncated is true when there were more entries than the limit.
	Truncated bool `json:"truncated"`
}

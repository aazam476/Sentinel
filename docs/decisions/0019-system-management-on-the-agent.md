# 0019 — System management on the agent

**Date:** 2026-11-08 · **Status:** Accepted

## Context

M3 adds services, processes, logs and power actions (§9.6). Most of these go through each
system's own command-line tools, with names (of services, for log filters) that come from the
server. Whoever controls the server already controls the machine, but a name must still never
be able to do more than the action it was given for, and the local policy's power switch
(SEC-22) must not be avoidable some other way.

## Decision

- **Programs run directly, never through a shell** (`internal/agent/command`), with names
  checked against what each service manager allows before use, and passed after `--` where
  the program takes options. Tests use a fake runner that answers only the exact command lines
  expected, so a change in how arguments are built fails a test.
- **systemd only accepts `*.service` names**, and refuses the services that reboot, power off
  or suspend (`systemd-reboot.service` and the like). Otherwise `systemctl start reboot.target`
  would reboot a machine whose policy switches power off.
- **The Windows Event Log is read with `wevtutil`**, not by calling wevtapi directly as the
  spec suggests. wevtutil ships with every Windows version and calls the same API; using it
  keeps one testable path (golden XML through the fake runner) for all three systems, with no
  hand-written system calls. Windows services use the Service Control Manager API, so no
  service name reaches a command line there.
- **macOS has no "manual" start mode**: a launchd job is automatic unless disabled.
  "Stop" sends SIGTERM (`launchctl kill`); launchd restarts a job marked KeepAlive, as it would
  after any exit. Unloading a job (`bootout`) would need its plist path and isn't offered.
- **Process CPU use is measured over one second**, from two snapshots, rather than since each
  process started, which says little about what it is doing now. A process list therefore
  takes about a second.
- **The agent won't end PID 1 or itself.**
- **Reboot and shutdown run two seconds after the agent answers**, so the server records that
  the request was accepted before the connection drops.
- **Two new operator permissions**: `manage_processes` (seeing and ending processes) and
  `control_power` (reboot, shutdown). §8.6 lets viewers see logs but doesn't mention
  processes; process command lines can carry secrets (database passwords, tokens), so seeing
  them is for operators. Services and logs stay visible to viewers. Every state-changing
  action is audited with its outcome, and a refusal by the local policy as "denied".
- **Logs**: entries come back newest first, at most 1,000 (default 200), from the last hour
  unless asked otherwise, with severities folded into five levels. `log show` on macOS has no
  limit or newest-first option, so the agent reads the whole window and keeps the newest.

## Consequences

- The spec's mention of wevtapi should say wevtutil (or "the Event Log API, through
  wevtutil").
- §8.6 should list processes and power under Operator.
- OpenRC (Alpine and others) isn't supported yet: services report "unsupported" where
  systemctl is missing. The spec lists OpenRC under Services.
- Golden files for systemd 255 unit files are captured from a real system; the others follow
  the documented formats of each tool and should be replaced with captures from real machines
  as they become available (the QA checklist covers this).

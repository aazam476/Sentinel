# 0022 — ZFS-aware storage, read-only

**Date:** 2026-11-08 · **Status:** Accepted

## Context

M5 adds a Storage tab (§9.5) that shows how a machine's space is really laid out. On ZFS,
per-disk and per-dataset `df` figures mislead: a mirror member looks full or empty depending on
the tool, and every dataset reports its pool's free space. The spec is firm that Sentinel only
reads ZFS, never changes it, and that a test must prove the agent can't run a command that does.

## Decision

- **The zfs and zpool CLIs with `-H -p`**, as the spec says: `zpool list` with named columns,
  `zpool status -P -p` (text, which every OpenZFS version prints; the `-j` JSON form is newer
  than many installed systems), `zfs list` with named columns plus `volsize` for zvols, and
  `zfs get` for one pool's compression. The parsers are fuzzed and tested against golden
  outputs from OpenZFS 0.8 and 2.2.
- **A guard in front of the command runner, in production, not only in tests.** Every zfs and
  zpool command passes through `readOnly`, which refuses any subcommand but `list`, `status`
  and `get`. Tests check the guard refuses each writing subcommand, and that everything the
  agent runs (against fixtures, and against real pools in the nightly ZFS job) is read-only.
- **Physical disks:** lsblk on Linux (JSON; older util-linux prints numbers as strings, and
  zvols, zram and loop devices are left out), `Get-PhysicalDisk` on Windows (whose own health
  status stands in for SMART), and smartctl on macOS, which has no other source that gives
  model and serial without a property-list parser. SMART comes from `smartctl --json` where
  smartmontools is installed, with `-n standby` so a sleeping disk isn't woken every minute.
  Its exit status is a bit mask that is non-zero for readable reports, so the output is read
  whatever the status.
- **Pool membership** follows each member's path in `zpool status -P` through its symbolic
  links (`/dev/disk/by-id/...`) to the partition, then to its disk.
- **Disk % in the fleet** counts pools (sent with each metrics sample) and non-ZFS filesystems.
  ZFS datasets and system mounts (tmpfs, overlay, squashfs snaps, container layers) are no
  longer sent as filesystems; a squashfs snap is always 100% full and made every Ubuntu
  machine with snaps look full.
- **Storage alerts come from the agent's storage health, not from the server polling.** The
  agent reads its storage when it connects and every 60 seconds, and sends a `storage.health`
  event when the summary changes (whole percentages, so a slowly filling pool doesn't send one
  every minute). The server raises and resolves alerts from it: pool not ONLINE, device errors,
  scrub errors, pool or quota above 80%, SMART failing or pre-fail warnings. The event is not
  in the spec's list (which has `zfs.pool.health_changed`); it covers SMART and quotas as well.
- **Alerts are stored now, in `internal/server/alerts`**, one row per condition, firing until
  it clears and then kept as resolved history. A partial unique index keeps one firing alert
  per condition. M6 adds rules, notifications, acknowledgement and maintenance mode on top.

## Consequences

- ZFS numbers come from the same commands an operator would run, so they match them exactly;
  the nightly job builds mirror and raidz pools from sparse files and compares every number
  with `zfs get` and `zpool get`.
- The storage overview runs a few commands per disk (smartctl). That is fine every minute on
  the machines Sentinel targets; a machine with dozens of disks would want a slower SMART
  interval.
- macOS lists disks only with smartmontools installed; the tab says so.
- ARC statistics are read on Linux only (`/proc/spl/kstat/zfs/arcstats`).

## The Storage tab

- One page, top to bottom as §9.5 orders it, after any firing storage alerts. Sizes are
  rounded for reading, in powers of 1,024 as `zfs list -h` shows them, with the exact byte
  count `zfs list -p` shows in each one's tooltip.
- The dataset list shows the tree or the largest first. It shows 200 datasets until asked for
  all: Docker's ZFS storage driver makes a dataset per image layer, which can be thousands.
- System mounts are hidden behind a "Show system mounts" toggle. Nothing on the tab animates:
  it refreshes every minute on a page people keep open.

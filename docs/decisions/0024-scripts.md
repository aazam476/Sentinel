# 0024 — Scripts and schedules

**Date:** 2026-11-09 · **Status:** Accepted

## Context

M6 adds a script library (§9.8): scripts in bash, sh, PowerShell or Python, run on one machine,
a tag or a hand-picked set, with each machine's output live, the exit code and duration kept,
and cron schedules with a readable preview. The "Done when" is a scheduled script running on a
tag of machines with its output viewable.

## Decision

- **The agent runs a script from a file in a new temporary folder only it can read**, with a
  fixed interpreter (`bash`, `sh`, `pwsh`/`powershell`, `python3`/`python`), never through a
  shell command line. Standard output and standard error share one pipe, so their order is
  kept. The script runs in a process group of its own on Unix, and the whole group is killed at
  its time limit (10 minutes by default, at most 24 hours) or when the server cancels it, so a
  background `sleep` can't outlive it. On Windows only the script's own process is ended; a job
  object would cover its children (a known limit).
- **Output flows on a stream** (§7.6): the agent answers `script.run` at once, sends output frames
  as they come, and closes the stream with the result (exit code, timed out, or why it couldn't
  run) as the close reason. The server closing the stream stops the script.
- **A batch keeps a copy of the script as it ran**, so editing or deleting a script never
  changes history. Each machine's run records its state (running, succeeded, failed, timed out,
  offline, skipped for the wrong system, canceled), exit code, error, times, and the first 1 MB of
  output. While a run is going, its output so far is kept in memory and the UI polls it every
  second; when it ends, it is stored.
- **Schedules use five-field cron**, with lists, ranges, steps, month and weekday names and the
  `@daily`-style shorthands, in a time zone chosen per schedule (the browser's by default), so
  "03:00" stays 03:00 across daylight saving. Cron's rule that a day matches if either the day
  of month or the weekday does (when both are given) is followed. The parser is Sentinel's own
  and fuzzed; the UI's readable preview uses cronstrue. The scheduler checks every minute for
  schedules that are due.
- **Permissions and audit**: operators and admins (`run_scripts`) manage and run scripts and
  schedules; viewers don't see them. Saving and deleting scripts and schedules and starting a
  batch (by a person or a schedule) are audited. The local policy can switch scripts off
  (SEC-22).

## Consequences

- A scheduled run on a machine that is offline at that minute is recorded as offline and isn't
  retried; the next scheduled time runs as usual.
- Output beyond 1 MB per machine is dropped, and the run says so.
- A failed scheduled script raises an alert once alert rules exist (M6-D).

# 0015 — Agent connections and metric storage

**Date:** 2026-11-08 · **Status:** Accepted

## Context

The spec fixes the handshake (§7.3), the heartbeat (15 s ping, offline after 45 s) and the
metric retention tiers (§9.1). How the server behaves around them was open: what it tells an
agent it refuses, what happens when an agent connects twice, how revocation reaches a live
connection, and how metrics are stored and rolled up.

## Decision

**Connections** (`internal/server/hub`)

- **Close codes say why.** A refused or ended connection is closed with a code from the
  application range (4001–4007, in `internal/protocol`): enrollment refused, unknown agent,
  revoked, handshake failed, unsupported version, replaced, heartbeat missed. The agent can tell
  "re-enroll" from "try again later" without parsing text.
- **Refused enrollments look the same to the agent.** A wrong, expired, revoked or used-up
  token gets one close code; the real reason goes only to the audit log.
- **Browsers are refused outright.** Agents never send an `Origin` header, so the endpoint
  refuses any request that has one, before the WebSocket upgrade.
- **A new connection replaces the old one.** An agent that reconnects before the server
  noticed its old connection died is accepted; the old connection is closed.
- **Revocation can't slip past.** Revoking stores the revocation, then closes the live
  connection. A handshake that was already past its revocation check re-checks right after
  registering its connection, so either the close or the re-check catches it.
- **The heartbeat watchdog compares times.** It records when the last message arrived and,
  when its timer fires, checks how long the connection has really been quiet. Resetting a
  timer on every message would race with the timer firing.
- **Online and offline are events.** The hub publishes an event when an agent comes online,
  goes offline or reports new machine details, so the UI can update live.

**Metrics** (`internal/server/metrics`)

- **One row per agent, metric and time** ("long" tables). A new metric needs no migration.
  Each sample yields CPU %, memory %, the fullest filesystem's %, network and disk rates, and
  the hottest temperature.
- **Rates are worked out on the server** from the agent's counters (totals since boot), by
  comparing consecutive samples. A counter that goes backwards (a reboot) gives no rate for
  that sample instead of a negative one.
- **Rollups recompute whole buckets.** Every minute, the minutes and hours that received
  samples since the last run are recomputed from scratch. An agent may deliver an hour of
  buffered samples late; recomputing keeps the averages right and makes a repeated run
  harmless. The hourly average is the average of its minutes.
- **Charts get a sensible number of points.** A query uses raw samples for up to 4 hours,
  minutes for up to 2 days, and hours beyond that, falling back to a coarser table when the
  range starts before the finer one's retention.
- **The newest sample stays in memory** for the fleet view, so listing the fleet doesn't query
  the metrics tables.
- **Samples from far outside "now"** (over an hour old, or more than 5 minutes ahead) are
  dropped, as are labels with hidden control characters.

## Consequences

- After a server restart the fleet view shows no current figures until each agent's next
  sample, at most 10 seconds later.
- Rollups after a restart cover the last two hours, so samples stored just before a crash are
  still rolled up.

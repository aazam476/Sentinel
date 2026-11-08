# 0016 — Live updates with server-sent events

**Date:** 2026-11-08 · **Status:** Accepted

## Context

The fleet and machine views need to change as soon as a machine comes online, goes offline,
or sends a metrics sample: live charts move every 10 seconds, and a revoked machine should
disappear at once. Polling every few seconds would be slow to react and wasteful.

## Decision

- **`GET /api/events` is a server-sent event stream** (`text/event-stream`). The browser's
  built-in `EventSource` reads it and reconnects by itself. It is one-way, which is all this
  needs, works through every proxy that passes ordinary HTTP, and needs no Origin check of its
  own: it is a GET that changes nothing, and the session cookie is `SameSite=Strict`.
- **Two event types.** `agent` (`{agentId, kind}` with kind `online`, `offline` or
  `info_updated`) tells the UI to refetch that machine; `metrics` (`{agentId, current}`)
  carries the new sample, so charts don't need a request per point.
- **The stream re-checks its session every 30 seconds** without counting as activity
  (`auth.CheckSession`): a revoked or timed-out session stops receiving events, and an idle tab
  doesn't stay signed in just because the stream is open. The same tick sends a comment line
  so proxies don't close a quiet connection; `X-Accel-Buffering: no` stops nginx from holding
  events back.
- **Shutdown ends the streams.** When the server stops, the hub closes every subscription and
  agent connection first, so the HTTP server's graceful shutdown doesn't wait on them.
- **The agents' endpoint skips the browser Origin check.** `/agent/connect` is routed before
  the CSRF middleware, because agents send no Origin header. The hub instead refuses any request
  that has one, which keeps browsers out entirely (SEC-13).

## Consequences

- A slow browser that falls more than 256 events behind misses some; the UI treats every event
  as "refetch" and charts tolerate a missing point.
- Each open tab holds one HTTP connection. Browsers allow six per host over HTTP/1.1, so users
  with many tabs open over plain HTTP/1.1 may notice; over HTTP/2 (TLS) this doesn't apply.

# 0050 — Only someone using the page keeps a session from going idle

**Date:** 2026-11-17 · **Status:** Accepted

## Context

SEC-06 ends a session after 12 hours idle. Every authenticated request restarted that clock,
including the ones a page makes on its own: the containers, processes, storage and script
pages poll, and every page refreshes data when the server announces a change. A tab left open
on one of them was never idle.

## Decision

- The browser notes when someone presses a key, clicks, touches or scrolls the page, and when
  it loads. For a minute after that, its API requests carry `X-Sentinel-Activity: 1`.
- Only requests with that header restart the idle timeout; others check the session without
  touching it. An open terminal or desktop session still counts as use (ADR 0044).
- The header grants nothing: a client that sends it on every request is back where every
  client was before.

## Consequences

- A tab nobody touches signs out 12 hours after its last use, whatever page it shows.
- Tools that call the API without the header must sign in at least every 12 hours.

# 0005 — End-to-end tests run the server binary directly until agents exist

**Date:** 2026-11-07 · **Status:** Accepted

## Context

End-to-end tests are meant to run Playwright against a Docker Compose "fleet": one server and
several agent containers. In the first milestone there is no agent and no server Docker image
yet, so a Compose fleet would contain only a server built in an unrelated way.

## Decision

Until agents exist, `make test-e2e` builds the real `sentinel-server` binary (with the web UI
embedded) and Playwright starts it directly through its `webServer` setting on
`127.0.0.1:7480`. The tests run in Chromium in four configurations: desktop width, phone
width (390 × 844), and each of those again with `prefers-reduced-motion: reduce`.

The Compose fleet replaces this once the agent can connect to a server.

## Consequences

- The tests exercise the same binary users run, with no Docker requirement for now.
- Only Chromium is tested automatically. Real iOS Safari and Android Chrome are covered by
  the manual QA checklist.

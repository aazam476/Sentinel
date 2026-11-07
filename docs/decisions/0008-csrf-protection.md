# 0008 — CSRF protection: SameSite=Strict cookies plus an Origin check

**Date:** 2026-11-07 · **Status:** Accepted

## Context

Every state-changing request must be protected against cross-site request forgery (SEC-12),
and WebSocket upgrades must check the `Origin` header (SEC-13). The two common approaches
are a double-submit token or `SameSite=Strict` cookies plus an `Origin` check.

## Decision

We use **`SameSite=Strict` session cookies plus a required `Origin` check**, in
`internal/server/middleware/origin.go`:

- Every request except `GET`, `HEAD` and `OPTIONS`, and every WebSocket upgrade, must carry an
  `Origin` header equal to the server's origin. A missing `Origin`, or `null`, is refused.
- The server's origin is `public_url` when configured. Otherwise it is the scheme and host the
  request was sent to (after trusted-proxy headers), so a fresh install works at whatever
  address the admin uses.
- `GET` requests never change state, so they are not checked.

## Consequences

- No token to fetch, store or send: every browser `fetch` already sends `Origin`.
- Two independent layers: a cross-site request carries no session cookie (`SameSite=Strict`),
  and even if it did, its `Origin` would be refused.
- Tools that don't send `Origin` (curl, scripts) can't call state-changing endpoints. That's
  intended: the API is for the web UI only. An API-token design would revisit this.
- Behind a proxy that rewrites the host, `public_url` must be set, or same-origin requests will
  be refused.

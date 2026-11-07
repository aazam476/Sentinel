# 0002 — Plain HTTP on loopback only until TLS is implemented

**Date:** 2026-11-07 · **Status:** Superseded by [0007](0007-server-configuration-and-tls.md)

## Context

The first milestone needs a running server that serves a page, but TLS (a self-signed
certificate by default, optional ACME) is part of the later authentication milestone. The
security requirements allow plain HTTP **only** when the server is bound to localhost or
explicitly marked as being behind a TLS-terminating proxy (SEC-10). The development setup is
also described as `https://localhost:7443`, which isn't possible without TLS.

## Decision

Until TLS exists, `sentinel-server` speaks plain HTTP and **refuses to start** on any address
that isn't loopback (`127.0.0.0/8`, `::1` or `localhost`). An empty host such as `:7443`,
which means "every interface", is refused too. The default address is `127.0.0.1:7443`.
`make dev` therefore serves `http://127.0.0.1:7443` for now.

When TLS is built, it becomes the default. Plain HTTP will then still be allowed on loopback,
and on other addresses only with the explicit "behind a TLS-terminating proxy" setting.

## Consequences

- An early build can never expose an unencrypted server to the network by accident.
- The check lives in `internal/server/httpserver` and is tested under SEC-10.
- The development URL changes from `http://` to `https://` once TLS lands; the docs and
  `scripts/run-dev.sh` will be updated then.

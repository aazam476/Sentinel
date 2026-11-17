# 0054 — Agents connect over plain HTTP only when told to

**Date:** 2026-11-17 · **Status:** Accepted

## Context

After the handshake, nothing but TLS protects the agent's connection (SEC-10). An agent
enrolled with an `http://` server address connected without TLS and said nothing, so anyone on
the network path could inject requests and run commands on the machine as root.

## Decision

- `sentinel-agent install` and `enroll` refuse an `http://` server unless `--insecure-http` is
  given. The install scripts pass it on (`--insecure-http`, `-InsecureHttp`).
- The Add machine page adds the flag only when the server is reached over `http://`, with a
  warning saying what it means.
- The hub records whether each agent connected without TLS; the machine's page warns while it
  is, and the agent logs a warning on every plain connection.
- Agents already enrolled over HTTP keep connecting: refusing them would leave machines that
  nobody can reach to fix.

## Consequences

- A plain-HTTP deployment needs one extra flag per machine, and shows a warning on every
  machine page.
- A TLS-terminating proxy is trusted like the server (it sees everything); `behind_tls_proxy`
  remains for that.

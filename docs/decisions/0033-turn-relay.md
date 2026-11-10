# 0033 — Remote desktop's built-in TURN relay

**Date:** 2026-11-10 · **Status:** Accepted

## Context

§9.3 step 4: when the browser and the machine can't reach each other directly, the screen
should go through the server's own TURN relay (Pion TURN) before falling back to the
WebSockets. ADR 0031 left the relay for M8-6.

## Decision

- **The server runs a Pion TURN relay** on UDP 3478 (`[turn] listen`; `"off"` turns it
  off). If its port is taken, the server logs it and runs without one: sessions still work,
  directly or through the WebSocket.
- **Credentials are made per session and expire** after eight hours (the TURN REST scheme):
  the username is the expiry time and a random name, the password an HMAC-SHA256 of it with
  a secret made at start-up and kept only in memory. Nothing is stored, and a restart makes
  old credentials useless. The agent gets them in `desktop.session.start`, the browser in
  the WebSocket's `ready` message, so both may use the relay.
- **The relay is named by IP address**, not the web UI's host name: a browser may reach the
  UI through a tunnel or proxy that carries no UDP, and the agent may know the server by
  another name. The address is `[turn] relay_ip` (or `--relay-ip`), else the one this
  machine uses to reach the internet. In Docker the container can't see the host's address,
  so the docs and `compose.yaml` pass it in.
- **Relayed connections use a fixed port range**, `[turn] relay_ports`, 49152–49251 by
  default, so a firewall or Docker can let exactly those through.
- **It relays only to peers, not to the server itself.** A signed-in user who opened a
  session holds working credentials, so the relay refuses loopback (unless the relay itself
  is on loopback, as in tests), link-local (cloud metadata services live there), multicast
  and unspecified addresses. Private network addresses are allowed: machines on the LAN
  are the point.
- **IPv4 only** for now.

## Consequences

- A tunnel that carries no UDP (Cloudflare Tunnel) still forces the WebSocket fallback; the
  relay helps where UDP works but a direct path doesn't (both ends behind NAT).
- Tests connect two relay-only Pion peers through it, check wrong, foreign and expired
  credentials are refused, check the port range, and check the address filter.

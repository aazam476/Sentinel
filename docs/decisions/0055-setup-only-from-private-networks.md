# 0055 — First-time setup only from this machine or a private network

**Date:** 2026-11-17 · **Status:** Accepted

## Context

ADR 0037 dropped the setup token, so until the first admin exists whoever reaches the server
first creates it. A server exposed to the internet before setup could be claimed by anyone, and
without `public_url` the origin check trusts the request's Host header, so a web page someone on
the LAN visits could rebind its own name to the server and run setup through their browser.

## Decision

- Setup's two steps are refused unless the client's address (after trusted proxies) is in
  `setup_allowed_from`. Empty, the default, means loopback, private ranges (10/8, 172.16/12,
  192.168/16, fc00::/7), the carrier-grade NAT range VPNs use (100.64/10) and link-local.
- Without `public_url`, setup is also refused unless the Host is an IP address, `localhost`, a
  name without dots, or a name under `.local`, `.lan`, `.home.arpa`, `.internal` or
  `.localhost`: names a rebinding page can't own.
- The refusal says what to do: open Sentinel from the machine or the LAN, or list an address,
  or set `public_url`.

## Consequences

- Setting up a server over the internet needs one line in `server.toml`, or an SSH tunnel.
- Once the first admin exists, nothing changes: sign-in works from anywhere.

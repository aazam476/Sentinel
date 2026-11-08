# 0007 — Server configuration and TLS

**Date:** 2026-11-07 · **Status:** Accepted

## Context

The server needs TLS on by default with a self-signed certificate, optional ACME, and plain
HTTP only on localhost or behind a TLS-terminating proxy (SEC-10). It also needs reverse-proxy
settings: public URL, base path and trusted proxies (SEC-14). These settings must exist before
the first admin can log in, so they can't live only in the database.

## Decision

**Configuration file.** Settings live in `server.toml` (default `/etc/sentinel/server.toml`,
or `--config`). A missing file means "all defaults". Unknown keys are an error, because a
silently ignored typo in a security setting is worse than a refusal to start. `--listen` and
`--data-dir` override the file, for scripts and containers.

```toml
listen = ":7443"                         # default
data_dir = "/var/lib/sentinel"           # default
public_url = "https://sentinel.example.com"
base_path = "/"                          # e.g. "/sentinel" behind a path-based proxy
trusted_proxies = ["10.0.0.0/8"]         # whose X-Forwarded-* headers to believe

[tls]
mode = "self-signed"                     # default; or "acme" or "off"
acme_email = ""
behind_tls_proxy = false                 # required for plain HTTP on a non-loopback address
```

**TLS modes.**

- `self-signed` (default): a P-256 certificate in `<data dir>/tls/`, valid for a year, covering
  localhost, the loopback addresses, the machine's host name, the public URL's host and the
  listen address. It is recreated when it is within 30 days of expiry, unreadable, or doesn't
  cover a needed host.
- `acme`: Let's Encrypt through the TLS-ALPN-01 challenge, cached in `<data dir>/tls/acme/`.
  Needs an `https://` public URL with a host name, and the server reachable on port 443.
- `off`: plain HTTP, only on a loopback listen address or with `behind_tls_proxy = true`.
  `behind_tls_proxy` with any other mode is refused, because it would mean double TLS.

**HSTS** is only sent in ACME mode. A browser that has seen HSTS won't let the user click past
a certificate warning, so sending it with a self-signed certificate would lock users out.

**Development** (`make dev`) runs the server with the self-signed certificate on
`https://127.0.0.1:7443` and sets `public_url` to the Vite dev server,
`http://localhost:5173` (a host name, because passkeys need one), which forwards API calls to the Go server.

## Consequences

- A fresh install is HTTPS from the first request, and an accidental plain HTTP setup on a
  network address refuses to start with an explanation.
- The settings page planned for later will show these values. Changing them there still
  needs a decision about writing back to the file or storing overrides in the database.
- Behind a path-based proxy, the web UI uses relative asset links and a `<base>` element the
  server adds, so one build works under any base path.

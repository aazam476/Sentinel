# 0041 — The Settings page: what admins change there, and what stays in server.toml

**Date:** 2026-11-12 · **Status:** Accepted

## Context

§9.10 calls for a Settings page. Some of the server's settings live in `server.toml` and are
read once at start: the listen address, public URL, base path, trusted proxies, TLS and ACME,
the release address, the largest file and the TURN relay. The server usually can't write that
file (it is owned by root or mounted read-only in Docker), changing any of them needs a
restart, and a wrong value can lock every admin out of the web UI they would use to fix it.

## Decision

- **Changed on the page, stored in the database:** how long metrics history is kept (raw
  samples 1–168 hours, minute averages 1–90 days, hourly averages 30–1,825 days, each tier at
  least as long as the one before) and the release channel (stable, or beta to include
  pre-releases). They take effect at once, without a restart.
- **Shown read-only:** everything from `server.toml`, with a note to change it there and
  restart.
- **Kept where they already are, and linked:** email (SMTP) and the other notification
  channels on Alerts → Channels, and the two-factor requirement on Users.
- Saving needs `manage_settings` (admins), a recent second factor when the user has one, and is
  audited as `settings.change` with the old and new values.
- **The release channel** decides where "Fetch the latest release" gets builds from when the
  release address is GitHub's `releases/latest/download/`: on beta the server asks the GitHub
  API for the newest release, pre-releases included. Without a saved choice it follows the
  server: a pre-release or development server is on beta, a stable one on stable.

## Consequences

- An admin can't break how the server is reached from the browser.
- Shortening retention deletes the older history at the next prune; it can't be brought back.

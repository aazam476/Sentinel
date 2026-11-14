# 0044 — Open connections end with the session that opened them

**Date:** 2026-11-14 · **Status:** Accepted

## Context

A terminal, a remote desktop session and a followed container log stay open long after the
request that started them was checked. Only the browser's event stream looked at its session
again (ADR 0016). So signing out, revoking a session (SEC-06), disabling a user or taking their
role away stopped new requests, but an open root shell or desktop session carried on, with no
limit, for whoever still held the connection.

## Decision

- **Each of these connections checks its session again every 30 seconds**, as the event stream
  does: it must still be signed in, and its role must still have the permission the connection
  needed (a terminal needs `use_terminal`, a container's shell `manage_docker`, a desktop
  session `use_desktop`, a followed log `view_fleet`).
- **When the check fails, the server closes the stream to the agent**, not just the browser's
  connection. The shell ends on the machine at once instead of waiting out the 30-second
  resume window, a desktop session stops being shared over WebRTC as well as through the
  server, and the browser is told why. The session's end is audited with the reason, "the
  viewer was signed out or lost access".
- **An open terminal or desktop session counts as the session being used**: its checks restart
  the idle timeout. Ending a shell ends what runs in it, so cutting one off after 12 hours
  without other requests would kill a restore or an upgrade someone left running. The
  seven-day limit still ends it. A followed log is not use, like the event stream: an idle
  tab showing one is still signed out.
- **A session that can't be read is not trusted**: the connection ends, and the server logs
  why.

## Consequences

- Up to 30 seconds pass between revoking a session and its shell closing. Telling open
  connections at once would need the auth service to know about them; polling keeps it as
  simple as the event stream.
- A terminal left open in a forgotten tab keeps its session signed in until the seven-day
  limit. Before, the session idled out after 12 hours while the shell stayed open regardless,
  which was worse.
- Scripts already started carry on when the person who started them signs out: a run belongs
  to its batch, not to a browser. A download in progress finishes.
- SEC-06 doesn't say what happens to open connections when a session ends, or whether they
  count as activity; both are listed for the maintainer as specification changes.

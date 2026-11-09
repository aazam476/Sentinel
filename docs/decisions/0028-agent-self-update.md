# 0028 — Agent self-update with rollback

**Date:** 2026-11-09 · **Status:** Accepted

## Context

§11.3 asks for the server to host the latest agent builds (fetched from the release channel, or
uploaded for air-gapped setups), for admins to update one machine, a tag or all, or turn on
automatic updates, and for the agent to download, verify, keep its previous binary, swap,
restart, confirm and roll back (SEC-31, SEC-32). The "Done when" is an agent updating itself
and rolling back automatically from a deliberately broken build.

A watchdog inside the new version can't catch a build that crashes before any of its code
runs, which is exactly what a broken build often does.

## Decision

- **The running agent tries the new build before installing it.** It downloads the build from
  its own server (`/install/updates/<version>/<file>`, fetched with the same pinned TLS as its
  connection; the server only says the path), checks the release signature in memory against the
  compiled-in keys (the agent, for this OS and architecture, of the version asked for), writes it
  next to itself, then pauses its own connection and runs the new build as a *trial*
  (`run --update-trial <file>`). The trial connects with the agent's identity; once its handshake
  succeeds it writes the file and exits. Only then does the old agent rename itself to
  `<executable>.previous`, move the new build into place, record the update and exit with code
  3, so the service manager starts the new version. If the trial fails, crashes or hasn't
  connected within five minutes, the build is deleted, the agent reconnects as it was, and it
  reports the failure. A broken build is therefore never installed.
- **A second guard after the swap.** The new version, finding an unconfirmed update record for
  itself, rolls back on its own if it hasn't connected within five minutes of starting: it moves
  itself aside (Windows can rename a running program but not replace it), restores the previous
  binary and restarts. Once it connects it confirms the update; the previous binary is deleted
  after a day.
- **Results reach the server as an `agent.update_result` event** when the agent next connects:
  updated, failed (with why) or rolled back. The server stores the last update per machine,
  audits it (`agent.update.result`, failure or success) and shows it on the machine page.
- **The server hosts builds** in `<data dir>/updates/<version>/`, each beside its signature,
  and keeps one only if the signature checks out with the server's own compiled-in keys. Admins
  upload a build and its `.sig`, or fetch the latest release: the server downloads the signed
  `SHA256SUMS`, checks it, then each agent build against both its checksum and its own
  signature. Builds are served to anyone under `/install/updates/`, as the install scripts will
  need. The release channel is `updates.release_url` in `server.toml`, the project's GitHub
  releases by default, or `off`.
- **Who updates what.** Admins (`manage_machines`) update one machine, a tag or all to the
  newest build for each machine's system, or a chosen version; a machine that is offline, has no
  build, already runs it, or is mid-update is skipped with the reason. With automatic updates on,
  the server checks every hour and updates machines to the newest build, but doesn't ask again
  for a version that failed on that machine: an admin retries by hand.

## Consequences

- During the trial the machine is briefly offline (seconds, normally).
- The agent needs write access to the folder its binary is in, which an installed agent (root,
  or LocalSystem) has.
- A machine that never reconnects after a rollback can't report it; the server shows the update
  as still in progress until it does.

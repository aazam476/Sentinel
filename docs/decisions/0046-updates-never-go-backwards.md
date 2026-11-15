# 0046 — An update to the newest build never goes backwards

**Date:** 2026-11-15 · **Status:** Accepted · **Supersedes:** "updates machines to the newest build" in ADR 0028

## Context

ADR 0028 has the server update machines "to the newest build for each machine's system": by
hand for one machine, a tag or all of them, and every hour when automatic updates are on. The
server asked any machine whose version differed from that build, not only one that was older.

A machine can run something newer than the server hosts: it was installed by hand from a later
release, or the builds here haven't been fetched for a while, or a build for one system was
uploaded without the others. Automatic updates then took that machine back to the older
build within the hour, without anyone asking, and with it every bug fixed since. The older
build is properly signed, so nothing on the agent refused it.

## Decision

- **"The newest build" only ever moves a machine forward.** When no version is named
  (automatic updates, and updating every machine or a tag from the Updates page), a machine
  that runs something newer than the newest hosted build is skipped, with the reason shown
  beside the others: "it runs v1.2.0, which is newer than the newest build here, v1.1.0".
- **Going back is for an admin to choose.** Naming a version, as the button on a machine's
  page and the API do, still installs exactly that version, older or not. That is how a bad
  release is backed out.
- Versions are compared as semantic versions where both are, and as text otherwise, the same
  way the list of builds is sorted.

## Consequences

- A fleet with automatic updates on can't be rolled back by hosting an older build alone; an
  admin updates the machines to that version by name.
- The agent itself still installs any properly signed build it is told to, older ones
  included. Refusing those on the machine, so that a taken-over server can't go back to a
  release with a known hole, needs a rule for what the lowest acceptable version is; that is a
  specification change, listed for the maintainer.

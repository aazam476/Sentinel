# 0004 — Version string from Go build information

**Date:** 2026-11-07 · **Status:** Accepted

## Context

Both binaries need a `version` subcommand, and the server logs its version at startup. The
usual Go approach sets a package-level `var version string` at link time with
`-ldflags "-X ..."`. Our rules forbid package-level mutable variables, and the linker trick is
invisible to someone reading the code.

## Decision

`internal/shared/buildinfo` reads the information the Go toolchain stamps into every binary
(`runtime/debug.ReadBuildInfo`):

- A build at a release tag reports the tag, for example `v0.1.0-beta.1` (Go sets the main
  module version from the git tag).
- Any other build reports `dev-<12-character commit hash>`, with `-dirty` added when the
  working tree had uncommitted changes.
- A binary without build information reports `dev`.

## Consequences

- No linker flags and no global variable; the release script only needs to build from a
  tagged commit.
- Builds from a source archive without `.git` report `dev`.

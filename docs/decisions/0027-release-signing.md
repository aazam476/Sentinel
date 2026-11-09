# 0027 — Release signing, checksums and SBOMs

**Date:** 2026-11-09 · **Status:** Accepted

## Context

SEC-30, SEC-31 and SEC-33 ask for every release artifact to be signed with an offline Ed25519
key compiled into both binaries, for updates to be checked before they are installed, and for
reproducible builds with checksums and an SBOM. The agent's self-update (M7) and the install
scripts depend on all three.

## Decision

- **A signature file per artifact.** `<artifact>.sig` is a short text manifest (format line,
  name, version, operating system, architecture, SHA-256) followed by an Ed25519 signature over
  exactly those lines. The fields are restricted to a safe character set and must appear in that
  order and no other, so there is one way to read a signature. Because the name, version and
  platform are signed, a valid signature for the Linux agent can't vouch for the Windows one, the
  server, or an older version.
- **Trusted keys are compiled in** from `internal/shared/release/keys/production.pub` (one base64
  key per line, so a new key can be added before an old one is retired). Until a production key
  is added there, builds trust `keys/development.pub` instead, whose private half is published
  in `testdata/release/` for tests and local experiments; `sentinel-agent version` and
  `sentinel-server version` say so, and `sentinel-release check-key` (run by the release script)
  refuses such a build. A broken key file makes a build trust nothing, so it can't install any
  update, and the tests catch it.
- **`sentinel-release`**, a maintainer's tool that isn't shipped, makes key pairs (`keygen`),
  signs (`sign`, key from `$SENTINEL_RELEASE_KEY`), checks (`verify`), lists checksums (`sums`)
  and writes SBOMs (`sbom`).
- **SBOMs are SPDX 2.3 JSON**, written by Sentinel's own code from the binary's Go build
  information (every module with its version and go.sum hash) and, for the server, the
  production packages of the web UI's `package-lock.json`. No extra tool is needed.
- **Builds are reproducible**: cgo off, `-trimpath`, an empty build ID, and the commit's time
  (`SOURCE_DATE_EPOCH`) for anything dated. A nightly job builds both binaries twice from two
  copies of the source in different folders and compares them byte for byte.
- **`scripts/release.sh <version>`** builds every platform, signs each binary, writes its SBOM,
  then lists the SHA-256 of everything in `SHA256SUMS` and signs that too.

## Consequences

- Before the first release, a maintainer must run `sentinel-release keygen` on an offline
  machine, commit the public key to `keys/production.pub`, and store the private key as the
  `SENTINEL_RELEASE_KEY` secret for the release workflow.
- Builds made from this repository without a production key will only accept updates signed
  with the published development key: fine for testing, never for real machines.

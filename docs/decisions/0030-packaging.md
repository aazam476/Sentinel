# 0030 — Packaging: the server image, the macOS installer and the release workflow

**Date:** 2026-11-09 · **Status:** Accepted

## Context

M7 asks for a Docker image, a signed and notarized macOS `.pkg`, and a release process that ties
together the signing, checksums and SBOMs of ADR 0027.

## Decision

- **The image contains the release's own binary**, copied from `dist/`, not built again inside
  Docker, so it runs exactly what was signed and reproducibly built. The base is
  `distroless/static` (no shell or package manager, CA certificates, tzdata, a nonroot user).
  The data directory is made in the image, owned by nonroot, so a new named volume mounted there
  is writable. The image is for `linux/amd64` and `linux/arm64`, pushed to
  `ghcr.io/aazam476/sentinel-server`, with `latest` only for non-prerelease versions.
- **The macOS installer** puts a universal `sentinel-agent` (`lipo` of both architectures) in
  `/Library/Sentinel/bin`, where §11.2 places it. Enrolling needs the one-time token, so the
  installer doesn't enroll; it says to run the Add machine page's command. With Developer ID
  certificates in the release secrets, the binaries are codesigned (hardened runtime,
  timestamp), the package is signed, notarized and stapled; without them it is built unsigned.
- **Codesigning changes the binary**, so `scripts/release.sh` has two halves, `build` and
  `sign`. The release workflow builds on Linux, codesigns the macOS binaries and builds the
  installer on a macOS runner, then signs everything with the release key, writes SBOMs and
  `SHA256SUMS`, checks every signature, and publishes. The codesigned macOS binaries therefore
  aren't byte-for-byte reproducible; everything else is (the nightly job checks), and the
  unsigned macOS builds are too.
- **A nightly job** builds a development-key release and the image, starts it, and checks it
  serves `/healthz` and prints the setup token. It doesn't run on every push, because it pulls
  base images.

## Consequences

- The first real release needs the production release key and, for a signed `.pkg`, an Apple
  Developer account; `docs/INSTALL.md` lists the secrets.
- `latest` moves with each stable release; pin a version tag in production.

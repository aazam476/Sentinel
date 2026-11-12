# 0040 — Apple and Windows code signing: off until a certificate is added

**Date:** 2026-11-12 · **Status:** Accepted

## Context

§11.4 planned a signed and notarized macOS installer and left Windows unsigned. The maintainer
decided to release without Apple signing or Windows code signing for now, and asked to keep
the option of turning them on later.

## Decision

- **Both are opt-in through repository secrets**, and the release workflow behaves the same
  with or without them: no workflow or code change is needed to switch either on.
- **macOS**, as before: with the Developer ID secrets `build-pkg.sh` codesigns the binaries
  with the hardened runtime and a timestamp, signs the installer and notarizes it; without
  them it builds an unsigned installer. The Go linker's ad-hoc signature still lets the agent
  run on Apple silicon.
- **Windows**, new: `scripts/sign-windows.sh` Authenticode-signs every `.exe` with
  osslsigncode, SHA-256 and an RFC 3161 timestamp, when `WINDOWS_CERTIFICATE_PFX` and
  `WINDOWS_CERTIFICATE_PASSWORD` are set; without them it says so and changes nothing. It runs
  on Linux in the publish job, before the release key signs, because Authenticode rewrites the
  file and the Ed25519 signature must cover what is published.
- **Sentinel's own signatures are unaffected**: every artifact is signed with the release key
  (SEC-30) and listed in `SHA256SUMS`, and agents verify updates against it (SEC-31), signed by
  Apple or Microsoft or not.

## Consequences

- Unsigned, macOS Gatekeeper blocks a double-clicked `.pkg` (right-click, Open, or allow it in
  System Settings; the install script avoids this), and Screen Recording and Accessibility
  must be granted again after each agent update, because macOS ties them to the signature.
  Windows shows SmartScreen's "More info, Run anyway", as it does today.
- Signed binaries are not byte-for-byte reproducible: the reproducibility check compares the
  unsigned builds, as it already did for macOS. To compare a signed Windows binary, remove its
  signature first (`osslsigncode remove-signature`).
- Tested with a throwaway self-signed certificate (sign, then `osslsigncode verify`), and
  without one (no change).

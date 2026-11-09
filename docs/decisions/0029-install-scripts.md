# 0029 — Install scripts that check signatures

**Date:** 2026-11-09 · **Status:** Accepted

## Context

§11.1 and §11.2 give one-line installs: `curl … | sudo sh` for Linux and macOS, `iwr … | iex`
for Windows, and a server install script. SEC-34 says the scripts verify the downloaded binary's
signature before installing.

## Decision

- **The server serves the agent scripts** at `/install/agent.sh` and `/install/agent.ps1`, with
  the release keys it trusts written in, and the newest build it hosts for each system at
  `/install/agent/<os>/<arch>/sentinel-agent` (and `.sig`). A script downloads both, checks the
  signed manifest names the agent and this system, that the build's SHA-256 matches, and that the
  Ed25519 signature is valid for one of the keys, and installs nothing otherwise. It then runs
  the build's own `install` with the enrollment flags, which enrolls the machine and installs
  the service (systemd, OpenRC or launchd on Unix, through the same library the agent already
  uses; the Windows service manager on Windows). Agent binaries are static (no cgo), so one
  build serves glibc and musl systems alike.
- **Linux and macOS check with `openssl pkeyutl -rawin`** (OpenSSL 1.1.1 or later), building the
  DER public key from a fixed prefix and the 32-byte key. A self-signed server is pinned with
  curl's `--pinnedpubkey`, from the certificate fingerprint the Add machine page already gives
  the agent. `curl` is told `--insecure` only together with the pin, which curl still enforces.
- **Windows checks in PowerShell itself**: Windows PowerShell 5.1 has no Ed25519, so the script
  carries a short RFC 8032 verifier on `System.Numerics.BigInteger` (about a second). The
  PowerShell one-liner can't pin a self-signed certificate, so for such a server the Add
  machine page offers the program's own `install` command instead (download
  `sentinel-agent.exe` and run it).
- **The server's own script** (`install-server.sh`, published with each release, keys written
  in by `scripts/release.sh`) finds the build for this machine in the signed `SHA256SUMS`, checks
  its signature the same way, then creates the `sentinel` user, installs the binary and a
  hardened systemd unit, writes a starter `server.toml`, starts it and prints the setup token.
- **Every script has `--dry-run`** (download and check, then describe what it would do), which
  is also how the tests run them: against a local server, with good, altered and wrongly-signed
  builds, and with a wrong certificate pin.

## Consequences

- A fresh server has no agent builds until an admin fetches the latest release (or uploads one)
  on the Updates page; the scripts say so if a machine asks first.
- macOS ships LibreSSL as `openssl`; where it lacks Ed25519 the script stops with a clear message,
  and the signed `.pkg` (M7) is the way in.

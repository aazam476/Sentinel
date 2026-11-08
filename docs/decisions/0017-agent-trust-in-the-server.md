# 0017 — How the agent trusts the server

**Date:** 2026-11-08 · **Status:** Accepted

## Context

The handshake (§7.3) proves the server's identity with its Ed25519 key, even through a proxy
that terminates TLS. But the handshake doesn't protect what follows: someone who can stand in
the middle of the TLS connection can relay the handshake and then read or change every message
after it, including terminal sessions. So the agent must also check the TLS certificate.

That is easy with a certificate from a real authority (ACME, or a proxy with its own
certificate), but the server's default is a self-signed certificate, which the usual check
rejects. Turning the check off would leave exactly the gap above.

## Decision

- **Without a pin, the agent checks the certificate as a browser would**, against the
  system's trusted authorities. This covers ACME and TLS-terminating proxies.
- **A self-signed server's install command carries a TLS fingerprint**:
  `--tls-fingerprint SHA256:...`, the SHA-256 of the certificate's public key. The agent then
  accepts only a certificate with that key, whoever signed it. The server shows the
  fingerprint next to the enrollment token whenever it serves its self-signed certificate.
- **The pin is the key, not the certificate**, and the server keeps its self-signed key when it
  renews the certificate (every 11 months, or when its host names change). Renewal therefore
  doesn't break agents.
- **The check runs on every TLS handshake, resumed sessions included** (`VerifyConnection`,
  not `VerifyPeerCertificate`, which resumed sessions skip).
- **The pin is saved with the agent's state** at enrollment, next to the server's Ed25519 key.

The agent's connection loop also reports what it is doing (connecting, connected, sampled,
waiting to retry) through an optional callback. Its tests use these reports to know when its
timers are running, instead of guessing from how many are armed, which was unreliable while a
connection wound down.

## Consequences

- Moving a server from its self-signed certificate to ACME (or a proxy) means re-running the
  agents' installs without `--tls-fingerprint`, or a later update that changes the saved pin.
- Deleting `<data dir>/tls/self-signed.key` changes the fingerprint; every agent then refuses
  the server until it is enrolled again. The data directory backup instructions should include
  it alongside `server-key`.

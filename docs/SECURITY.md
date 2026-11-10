# Security

## Reporting a vulnerability

Please **don't** open a public issue for a security problem. Report it privately through
GitHub's [private vulnerability reporting](https://github.com/aazam476/sentinel/security/advisories/new)
for this repository.

Include what you found, how to reproduce it, and which version or commit you tested. You'll
get an acknowledgement within 7 days. Once a fix is released, we'll credit you in the release
notes unless you'd rather stay anonymous.

## What to keep in mind

Sentinel is a remote-access tool, so **whoever controls the server controls every machine
with an agent on it.** Protect the server like you would protect a domain controller:
keep it updated, put it behind a reverse proxy or a private network if you can, and keep the
number of admin accounts small.

Two features limit the damage a compromised server can do:

- The **local policy file** on each machine can switch off any capability: terminal, remote
  desktop, files, scripts, Docker, storage, packages or power. It lives at
  `/etc/sentinel/policy.toml` (Linux), `/Library/Sentinel/etc/policy.toml` (macOS) or
  `C:\ProgramData\Sentinel\etc\policy.toml` (Windows); the installer writes a commented
  starter file. The server can't change it, and the agent refuses to start if the file can be
  changed by anyone other than root, or if it has a typo, so a mistake can never switch a
  capability back on. The web UI shows switched-off capabilities as "Disabled by local
  policy".
- **Consent prompts.** With `desktop_requires_consent = true` in the policy file, the
  person logged in at the machine is asked, by name of whoever is connecting, to allow or deny
  each remote desktop session. With no answer in 25 seconds, or no one to ask, it is
  refused. Where the agent can't ask yet (Windows, macOS and Wayland, for now), the setting
  refuses every session.
- **A session indicator.** With `show_session_indicator = true`, a bar at the top of the
  screen says who is connected while any remote terminal or desktop session is open.

Both are local policy settings, so the server can't turn them off:

```toml
desktop_requires_consent = true
show_session_indicator = true
```

## How agents and the server trust each other

- Each agent has its own Ed25519 key, kept in a directory only root/SYSTEM can read. Every
  connection starts with both sides signing the other's random challenge, so neither can be
  impersonated, even through a proxy that terminates TLS.
- The install command carries the server key's fingerprint, and, for a server using its own
  self-signed certificate, the certificate key's fingerprint. Copy it exactly: the agent
  refuses a server that doesn't match.
- Agents only ever connect out; they never listen on a network port.
- Revoking a machine in the web UI disconnects it at once; it can't reconnect until it is
  added again.

Back up `secret.key`, `server-key` and `tls/self-signed.key` with the data directory: losing
`server-key` (or the self-signed key, if you use it) means every agent must be added again.

## Supported versions

Sentinel has no release yet. Once `v0.1.0-beta.1` ships, the latest release receives
security fixes.

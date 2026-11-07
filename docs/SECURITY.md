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

- The **local policy file** on each machine (owned by root/SYSTEM) can switch off any
  capability: terminal, remote desktop, files, scripts, Docker, storage, packages or power.
  The server can't change it.
- **Consent prompts** can require the logged-in user to approve each remote desktop session.

Both are planned and documented here as they are built.

## Supported versions

Sentinel has no release yet. Once `v0.1.0-beta.1` ships, the latest release receives
security fixes.

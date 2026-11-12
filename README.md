# Sentinel

Sentinel is a self-hosted, cross-platform remote monitoring and management (RMM) tool.
One central web dashboard, a lightweight agent on every machine, and first-class support
for terminals, Docker, ZFS-aware storage views and remote desktop.

- **Agents connect outbound** to the server, so managed machines need no open ports and
  work behind NAT.
- **Linux, macOS and Windows** agents; the server runs anywhere Docker or Linux runs.
- **Secure by default:** TLS, two-factor authentication (optional, or required by an admin), a tamper-evident audit
  log, signed updates, and a local policy file on every machine that the server can't
  override.
- **Readable code.** A student should be able to open any file and understand it.

> **Status: beta candidate.** Everything planned for `v0.1.0-beta.1` is built: first-boot
> setup, sign-in with two-factor authentication (TOTP or passkeys) that admins can require, the audit log,
> machines with live and historical metrics, the terminal, services, processes, logs and
> power, Docker and compose stacks, ZFS-aware storage, the file manager, scripts and
> schedules, alerts and notifications, users, backups, signed self-updating agents, and
> remote desktop, and a Sentinel icon in the Windows notification area and the macOS menu bar
> for the person at the machine ([ADR 0038](docs/decisions/0038-tray.md)). Remote desktop and
> the tray on Windows and macOS still need testing on real machines, and Wayland desktops
> aren't supported yet ([ADR 0036](docs/decisions/0036-desktop-helper-windows-macos.md)).

## Quick start (development)

You need Go 1.27+, Node.js 22+, `make` and bash.

```bash
git clone https://github.com/aazam476/sentinel.git
cd sentinel
make dev
```

Then open <http://localhost:5173>, the web UI with hot reload in front of the server.

To connect a machine while developing, build the agent with `make build`, open **Add machine**,
create an install command, and run it with `enroll` instead of `install`, keeping the agent's
files in the repository instead of system folders:

```bash
bin/sentinel-agent enroll --server http://localhost:5173 --token TOKEN --fingerprint FP --state-dir .dev/agent/state --config-dir .dev/agent/etc
```

```bash
bin/sentinel-agent run --state-dir .dev/agent/state --config-dir .dev/agent/etc
```

## Documentation

| Document | What's in it |
|---|---|
| [docs/INSTALL.md](docs/INSTALL.md) | Installing the server and agents, updates, backups, releases |
| [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) | Setting up, running tests, house rules |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | How the pieces fit together |
| [docs/GLOSSARY.md](docs/GLOSSARY.md) | The words we use |
| [docs/DEPENDENCIES.md](docs/DEPENDENCIES.md) | Every third-party dependency and why it's here |
| [docs/SECURITY.md](docs/SECURITY.md) | Reporting a vulnerability, and what limits a compromised server |
| [docs/SECURITY-REVIEW.md](docs/SECURITY-REVIEW.md) | Every security requirement, how it is built and how it is tested |
| [docs/QA-CHECKLIST.md](docs/QA-CHECKLIST.md) | The manual checks before a release |
| [docs/decisions/](docs/decisions/) | Architecture decision records |

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

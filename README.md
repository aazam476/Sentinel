# Sentinel

Sentinel is a self-hosted, cross-platform remote monitoring and management (RMM) tool.
One central web dashboard, a lightweight agent on every machine, and first-class support
for terminals, Docker, ZFS-aware storage views and remote desktop.

- **Agents connect outbound** to the server, so managed machines need no open ports and
  work behind NAT.
- **Linux, macOS and Windows** agents; the server runs anywhere Docker or Linux runs.
- **Secure by default:** TLS, mandatory two-factor authentication, a tamper-evident audit
  log, signed updates, and a local policy file on every machine that the server can't
  override.
- **Readable code.** A student should be able to open any file and understand it.

> **Status: early development.** The server, first-boot setup, sign-in with mandatory
> two-factor authentication (TOTP or passkeys), the audit log, and connecting machines
> (the agent, enrollment, live and historical metrics, tags and revocation) work. Terminal,
> Docker, storage, files and remote desktop don't yet. The project is being built milestone
> by milestone towards a single beta release, `v0.1.0-beta.1`.

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

Screenshots will be added here once there is a dashboard to show.

## Documentation

| Document | What's in it |
|---|---|
| [docs/INSTALL.md](docs/INSTALL.md) | Installing the server and agents, updates, backups, releases |
| [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) | Setting up, running tests, house rules |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | How the pieces fit together |
| [docs/GLOSSARY.md](docs/GLOSSARY.md) | The words we use |
| [docs/DEPENDENCIES.md](docs/DEPENDENCIES.md) | Every third-party dependency and why it's here |
| [docs/SECURITY.md](docs/SECURITY.md) | Reporting a vulnerability |
| [docs/decisions/](docs/decisions/) | Architecture decision records |

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

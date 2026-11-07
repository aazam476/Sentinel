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

> **Status: early development.** Nothing is usable yet. The project is being built
> milestone by milestone towards a single beta release, `v0.1.0-beta.1`.

## Quick start (development)

You need Go 1.27+, Node.js 22+, `make` and bash.

```bash
git clone https://github.com/aazam476/sentinel.git
cd sentinel
make dev
```

Then open <http://127.0.0.1:5173>, the web UI with hot reload in front of the server.

Screenshots will be added here once there is a dashboard to show.

## Documentation

| Document | What's in it |
|---|---|
| [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) | Setting up, running tests, house rules |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | How the pieces fit together |
| [docs/GLOSSARY.md](docs/GLOSSARY.md) | The words we use |
| [docs/DEPENDENCIES.md](docs/DEPENDENCIES.md) | Every third-party dependency and why it's here |
| [docs/SECURITY.md](docs/SECURITY.md) | Reporting a vulnerability |
| [docs/decisions/](docs/decisions/) | Architecture decision records |

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

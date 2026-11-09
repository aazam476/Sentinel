# Installing Sentinel

Sentinel is two programs: **sentinel-server**, which you run once (in Docker or on a Linux
machine), and **sentinel-agent**, which you install on every machine you want to manage.

Every release file is signed with Sentinel's Ed25519 release key, and both programs and every
install script check those signatures before installing anything (see
[SECURITY.md](SECURITY.md)). Releases also carry a `SHA256SUMS` file and an SBOM per binary.

## The server

### With Docker

```bash
docker run -d --name sentinel --restart unless-stopped \
  -p 7443:7443 \
  -v sentinel-data:/var/lib/sentinel \
  ghcr.io/aazam476/sentinel-server:latest
```

```bash
docker logs sentinel
```

The log shows a one-time **setup token**. Open `https://<this host>:7443`, accept the
self-signed certificate, and enter the token to create the first admin. You set up two-factor
authentication as part of it.

With Docker Compose, use [`compose.yaml`](../compose.yaml) from the repository:

```bash
docker compose up -d
```

```bash
docker compose logs sentinel
```

The image holds only the release's signed binary, on a distroless base, and runs as a
non-root user. Keep the `sentinel-data` volume: it holds the database, the keys and the
certificate. Losing it means enrolling every agent again.

### On Linux, natively

```bash
curl -fsSL https://github.com/aazam476/sentinel/releases/latest/download/install-server.sh | sudo sh
```

The script downloads the server for this machine and checks its signature with `openssl`. It
then creates a `sentinel` system user and installs `/usr/local/bin/sentinel-server`. It writes
`/etc/sentinel/server.toml` (unless you already have one), creates `/var/lib/sentinel` with
mode 0700, and starts a hardened systemd service. At the end it prints the address and the
setup token. It needs systemd, `curl` and OpenSSL 1.1.1 or later. Add `--dry-run` to check the
download without installing anything.

To install by hand, download `sentinel-server_<version>_linux_<arch>` and its `.sig` from the
release, check it, and run it:

```bash
sentinel-release verify sentinel-server_v1.2.3_linux_amd64
```

```bash
sentinel-server --config /etc/sentinel/server.toml
```

### Settings

Everything is optional; these are the defaults:

```toml
listen = ":7443"
data_dir = "/var/lib/sentinel"
# The address people type, when a proxy changes the host or port.
public_url = ""
# Serve Sentinel under a sub-path, e.g. "/sentinel".
base_path = ""
# Proxies whose X-Forwarded-* headers are believed (IPs or CIDR ranges).
trusted_proxies = []

[tls]
# "self-signed" (the default), "acme" (Let's Encrypt), or "off".
mode = "self-signed"
acme_email = ""
# Plain HTTP on a non-loopback address only when a proxy in front does HTTPS.
behind_tls_proxy = false

[files]
# The largest file the file manager moves, in GiB.
max_size_gb = 10

[updates]
# Where agent builds are fetched from; "off" for air-gapped servers, which
# upload builds on the Updates page instead.
release_url = "https://github.com/aazam476/sentinel/releases/latest/download/"
```

With the self-signed certificate, the Add machine page puts the certificate's fingerprint in
every install command, so agents pin it. With ACME or a proxy, agents check the certificate the
usual way.

### Backups

```bash
sudo sentinel-server backup --passphrase-file /root/backup-passphrase /root/sentinel.backup
```

```bash
sudo sentinel-server restore --passphrase-file /root/backup-passphrase /root/sentinel.backup
```

A backup holds the database, the keys, the certificates and `server.toml`, encrypted with the
passphrase (at least 12 characters). Keep the passphrase: without it the backup can't be
restored. Both commands run only as root or as the data directory's owner. A backup can be
taken while the server runs. To restore over an existing server, stop
it first and add `--force`. See ADR 0026 for the format.

## The agent

Before adding the first machine, the server needs agent builds. On **Updates**, choose **Fetch
the latest release**, or upload a build and its `.sig` if the server can't reach the internet.

Then go to **Machines → Add machine** and choose **Create install command**. The command
carries a one-time enrollment token and the server's key fingerprint, so copy it exactly. It
works for the number of machines and the time you chose.

### Linux and macOS

```bash
curl -fsSL https://<server>/install/agent.sh | sudo sh -s -- \
  --server https://<server> --token <token> --fingerprint <fp>
```

The script downloads the agent from your server and checks its signature with `openssl` against
the keys the server trusts. It then installs it to `/usr/local/bin` (`/Library/Sentinel/bin` on
macOS) and runs `sentinel-agent install`. That enrolls the machine and starts the service:
systemd or OpenRC on Linux, launchd on macOS. Agent binaries are static, so the same one works
on glibc and musl (Alpine) systems. With a self-signed server, the command also pins the
server's certificate (`--pinnedpubkey`).

On macOS the system's `openssl` is LibreSSL, which may not check Ed25519 signatures. If the
script stops there, use the installer below, or Homebrew's `openssl`.

**macOS installer:** download `sentinel-agent_<version>_darwin.pkg` from the release and
double-click it. It is signed with a Developer ID and notarized. It works on Apple silicon and
Intel. Then run the `sentinel-agent install …` command from the Add machine page. macOS still
asks you to allow some features (screen recording, for remote desktop) in System Settings.

### Windows

In PowerShell, run as Administrator:

```powershell
& ([scriptblock]::Create((iwr -UseBasicParsing https://<server>/install/agent.ps1).Content)) -Server https://<server> -Token <token> -Fingerprint <fp>
```

The script checks the agent's signature in PowerShell itself, installs it to
`C:\Program Files\Sentinel`, and starts the **Sentinel Agent** service.

With a self-signed server, the Add machine page shows a different command instead, because
PowerShell can't pin the certificate. Download `sentinel-agent.exe` from the release (and check
it with `sentinel-release verify`), then run it as Administrator:

```powershell
.\sentinel-agent.exe install --server https://<server> --token <token> --fingerprint <fp> --tls-fingerprint <tls-fp>
```

Windows builds aren't codesigned with a Microsoft certificate (§11.4), so expect the following.
Sentinel's own signatures still guarantee the file is genuine.

- **SmartScreen.** Opening a downloaded `sentinel-agent.exe` may show "Windows protected your
  PC". Choose **More info**, then **Run anyway**. The PowerShell command avoids this: files
  that `Invoke-WebRequest` downloads aren't marked as coming from the internet.
- **Smart App Control.** On a fresh Windows 11 installation with Smart App Control on, unsigned
  programs are blocked outright. Turn it off, or install on a machine where it's off.
- **Antivirus.** Remote management tools are sometimes flagged. Maintainers submit each release
  to Microsoft's false-positive portal. If yours quarantines the agent, allow
  `C:\Program Files\Sentinel\sentinel-agent.exe`.

### What the agent may do

Each machine has a local policy file, `policy.toml`, that only someone on that machine can
change. It can turn features off, such as the terminal, files, scripts, Docker or power, and
limit the file manager to some folders. It lives in `/etc/sentinel/` on Linux,
`/Library/Sentinel/etc/` on macOS, and `C:\ProgramData\Sentinel\etc\` on Windows. The machine
page shows what the policy turns off.

### Updates

On **Updates**, update every machine, or open a machine and choose **Update to …**. You can
also turn on automatic updates, which run hourly. Each machine checks the new build's
signature, then tries it while keeping the old one. It only switches if the new version
connects, and otherwise reports why and keeps running. See ADR 0028.

### Removing the agent

```bash
sudo sentinel-agent uninstall
```

On Windows, run `sentinel-agent.exe uninstall` as Administrator. This removes the service, the
local policy and the agent's keys; delete the binary afterwards (`/usr/local/bin/sentinel-agent`,
`/Library/Sentinel/bin/sentinel-agent` or `C:\Program Files\Sentinel`). Then revoke the machine
on its page, so the server stops expecting it.

## For maintainers: making a release

1. Once, on an offline machine, run `sentinel-release keygen release.key`. Add the public key it
   prints to `internal/shared/release/keys/production.pub` and commit it. Store the contents of
   `release.key` as the `SENTINEL_RELEASE_KEY` secret, and keep the file itself offline. Until a
   production key is committed, builds trust only the published development key, and the
   release workflow refuses to run.
2. For a signed and notarized macOS installer, add the Apple secrets listed in
   `.github/workflows/release.yml`. Without them the installer is unsigned.
3. Tag the commit (`git tag v1.2.3 && git push origin v1.2.3`). The release workflow builds
   every binary reproducibly, codesigns the macOS ones, signs everything with the release key,
   writes SBOMs and `SHA256SUMS`, publishes the GitHub release, and pushes the image to
   `ghcr.io`.
4. Submit the Windows binaries to Microsoft's false-positive portal (§11.4).

To try a release locally with the development key:

```bash
SENTINEL_RELEASE_KEY="$(cat testdata/release/development.key)" ALLOW_DEVELOPMENT_KEY=1 scripts/release.sh all v0.0.0-test linux/amd64
```

Never publish what this produces.

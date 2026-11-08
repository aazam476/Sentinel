# Manual QA checklist (beta)

Some things can't be automated because a person has to click an operating system dialog or
hold a real device. Before the beta release, someone works through every item below and
records the result.

Record each run as: **pass/fail · OS and version · date · tester · notes**.

## macOS

- [ ] `.pkg` installs by double-clicking
- [ ] Screen Recording permission prompt appears and remote desktop works after granting it
- [ ] Accessibility permission prompt appears and remote input works after granting it
- [ ] After denying a permission, the UI explains how to re-enable it, and re-enabling works

## Windows

- [ ] SmartScreen flow for the downloaded `.exe` ("More info → Run anyway")
- [ ] Defender behavior on install and first run
- [ ] A UAC prompt is visible and usable over remote desktop
- [ ] The login screen is reachable over remote desktop

## Linux Wayland

- [ ] The portal consent dialog appears on GNOME and remote desktop works after approving
- [ ] The same on KDE

## Real hardware

- [ ] SMART readings and temperatures are correct on physical disks

## Agents

- [ ] `sentinel-agent install` sets up and starts the service on Debian/Ubuntu (systemd), macOS (launchd)
  and Windows (service manager), and the machine appears online
- [ ] The agent comes back by itself after the machine reboots
- [ ] `sentinel-agent uninstall` removes the service and its files on each of those
- [ ] On Windows, a non-administrator can't open `C:\ProgramData\Sentinel\state` (SEC-20)

## Real reverse proxies

- [ ] Caddy
- [ ] nginx
- [ ] Traefik
- [ ] Cloudflare Tunnel
- [ ] Tailscale Funnel

## Captured command output

The parsers' golden files in `internal/agent/services/testdata` and
`internal/agent/logs/testdata` follow each tool's documented format; only systemd 255's unit
file list was captured from a real machine. On each system, capture the real output, add it
next to the existing files (named after the system and version) and add a test case for it.

- [ ] `systemctl list-units` and `list-unit-files` on Debian 12, RHEL 9, Ubuntu 20.04
- [ ] `journalctl -o json` with a binary message
- [ ] `launchctl list` and `launchctl print-disabled system` on macOS 13, 14 and 15
- [ ] `log show --style ndjson` on macOS 14
- [ ] `wevtutil qe System /f:RenderedXml` on Windows 10, 11 and Server 2022

## Docker

- [ ] Docker Engine on Debian and Ubuntu: containers, images, volumes, networks, a stack from a pasted compose file, logs, exec, pull and redeploy
- [ ] Podman (socket set in the policy file) on Fedora
- [ ] Docker Desktop on macOS and Windows; OrbStack and Colima on macOS
- [ ] A private registry login (ghcr.io): pull and update check
- [ ] "Docker not detected" on a machine without Docker

## Phones

- [ ] Layout and touch behavior on iOS Safari
- [ ] Layout and touch behavior on Android Chrome

## Results

| Item | Result | OS / version | Date | Tester | Notes |
|---|---|---|---|---|---|

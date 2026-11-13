# 0042 — Operating system updates: one agent package per manager, and a daily digest

**Date:** 2026-11-13 · **Status:** Accepted

## Context

§9.6 asks for package updates on every platform (apt, dnf, zypper, pacman and apk on Linux;
`softwareupdate` and Homebrew on macOS; Windows Update and winget on Windows), and §9.9 for an
"updates available" alert sent as a daily digest. Each manager prints differently, runs as a
different user, and can't safely be stopped halfway.

## Decision

- **`internal/agent/packages`** finds the machine's managers by their programs, runs them
  through `command.Runner` without a shell, and parses their output with fuzzed parsers.
  Linux has one manager (the first found); macOS has `softwareupdate` and, when installed,
  Homebrew; Windows has Windows Update and, when installed, winget.
- **IDs are checked before they reach a program**: package names may not start with `-` or
  contain spaces, macOS update labels are limited to the characters Apple uses, and Windows
  Update IDs must be GUIDs, because they are written into the PowerShell script. Parsers drop
  anything their manager would then refuse.
- **Windows Update** is driven through its COM API (`Microsoft.Update.Session`) from
  PowerShell, which avoids a cgo or COM binding in the agent. winget is found in its
  `WindowsApps` folder, since the agent's SYSTEM account doesn't have it on its PATH.
- **Homebrew** refuses to run as root, so the agent runs it with `sudo -u` as the user who
  owns the `brew` program. A root-owned Homebrew is left out.
- **pacman** only installs everything at once (Arch doesn't support partial upgrades); the
  protocol says so (`selectable: false`) and the UI offers no checkboxes there. Listing uses
  `checkupdates`, which never touches the real package database.
- **An install isn't tied to its connection**: if the server or browser goes away, the
  package manager carries on, for up to two hours, rather than being killed halfway.
- **Permissions**: listing and installing need the new `manage_packages` permission
  (operators), because even listing refreshes catalogs as root. Installing needs a recent
  second factor, as the terminal does (SEC-07), and is audited (SEC-40). The local policy's
  `packages` capability switches both off (SEC-22); a machine without a supported manager
  reports the capability off with that reason, so the UI shows no Updates tab (§10.2).
- **The digest**: once a day the server asks every online machine for its updates and raises
  or resolves an `updates.available` alert per machine, without notifying. It then sends each
  "updates" rule's channels one notification naming every machine with updates waiting (up to
  20, then a count), leaving out machines in maintenance. Migration 0012 adds an "Updates
  available" rule for every machine, which can be edited or switched off.

## Consequences

- Machines that can't be checked (offline, no manager, policy off) keep their last alert
  state until they can be.
- The digest repeats daily while updates wait; switching the rule off stops it.

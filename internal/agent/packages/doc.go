// SPDX-License-Identifier: Apache-2.0

// Package packages lists and installs operating system updates (§9.6)
// through the package managers a machine has: apt, dnf, zypper, pacman or
// apk on Linux, softwareupdate and Homebrew on macOS, and Windows Update
// and winget on Windows. Each manager is found by looking for its program,
// run through a command.Runner without a shell, and its output parsed by
// a function that is fuzzed. Update IDs are checked before they are passed
// to any program.
package packages

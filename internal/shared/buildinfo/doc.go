// SPDX-License-Identifier: Apache-2.0

// Package buildinfo reports which version of Sentinel is running.
//
// Both binaries call Version for their "version" subcommand and log it at
// startup. The version comes from the information the Go toolchain stamps into
// every binary (module version from the git tag, commit hash, and whether the
// working tree had uncommitted changes), so release builds need no extra
// linker flags and there is no package-level variable to set.
package buildinfo

// SPDX-License-Identifier: Apache-2.0

// Package storage reads how a machine's storage is laid out (§9.5): ZFS
// pools, their datasets and vdevs, the ARC, physical disks with SMART and
// the pool each belongs to, and other filesystems.
//
// It only ever reads. Every zfs and zpool command goes through readOnly,
// which refuses any subcommand but list, status and get, so a command
// that changes ZFS can't be run from here even by mistake. ZFS has no
// stable Go API; its CLIs with -H -p are its script-friendly interface.
package storage

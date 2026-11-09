// SPDX-License-Identifier: Apache-2.0

// Package updates hosts agent builds and updates machines with them (§11.3).
//
// Builds live in <data dir>/updates/<version>/ beside their release
// signatures, and are only kept if the signature checks out (SEC-31); the
// server serves them, unauthenticated, under /install/updates/, for agents
// updating themselves and for install scripts. Admins add builds by
// uploading them or by fetching the latest release, then update one
// machine, a tag or all of them, or let machines update by themselves.
// Each machine reports how its update went (SEC-32), and that is recorded
// and audited.
package updates

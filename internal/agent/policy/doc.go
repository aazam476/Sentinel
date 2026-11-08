// SPDX-License-Identifier: Apache-2.0

// Package policy reads the machine's local policy file (SEC-22):
// /etc/sentinel/policy.toml on Linux, or the platform's equivalent.
//
// The file belongs to root and the server can't change it, so it limits
// what even a compromised server can make this machine do. It can switch
// off any capability:
//
//	[capabilities]
//	terminal = false
//	desktop  = false
//
// Every capability is allowed unless the file switches it off, and a
// machine without a policy file allows everything. A file that can't be
// trusted or understood stops the agent from starting, rather than being
// ignored: a typo must never re-enable something an admin switched off.
package policy

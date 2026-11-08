// SPDX-License-Identifier: Apache-2.0

// Package command runs the external programs the agent relies on, such as
// systemctl, journalctl and launchctl. Features depend on the Runner
// interface, so their tests can use a fake that checks the exact arguments
// and returns output captured from real machines.
package command

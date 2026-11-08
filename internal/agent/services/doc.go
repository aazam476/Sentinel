// SPDX-License-Identifier: Apache-2.0

// Package services lists and controls the machine's services (§9.6):
// systemd units on Linux, launchd jobs on macOS and the Service Control
// Manager on Windows. Each one's states and startup modes are mapped onto
// the few the UI shows.
//
// Service names come from the server, so each backend checks them against
// the characters its service manager allows before using them, and passes
// them after "--" where the program takes options.
package services

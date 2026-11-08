// SPDX-License-Identifier: Apache-2.0

// Package setup puts the agent on a machine and takes it off again: the
// steps behind `sentinel-agent enroll`, `install` and `uninstall`.
//
// Install enrolls the agent with its server, saves its identity, writes a
// starter policy file if there is none, and installs and starts the
// operating system service (systemd, launchd or the Windows service
// manager). Uninstall stops and removes the service and deletes the
// agent's state and configuration.
//
// Every step says what it does as it goes. With DryRun the steps are only
// described: nothing is changed, and the enrollment token isn't used up.
package setup

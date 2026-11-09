// SPDX-License-Identifier: Apache-2.0

// Package install serves the agent install scripts (§11.2) and the newest
// agent build for each system.
//
// The scripts (agent.sh for Linux and macOS, agent.ps1 for Windows) carry
// the release keys this server trusts. Each downloads the newest build the
// server hosts for its machine, checks the build's release signature
// against those keys before installing anything (SEC-34), then runs the
// agent's own `install` with the enrollment flags from the Add machine
// page.
package install

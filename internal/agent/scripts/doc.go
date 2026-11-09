// SPDX-License-Identifier: Apache-2.0

// Package scripts runs scripts from the server's library (§9.8): bash, sh,
// PowerShell or Python, each from a file only the agent can read, with its
// output (standard output and standard error, in the order written) passed
// on as it comes, and stopped, with everything it started, at its time
// limit.
package scripts

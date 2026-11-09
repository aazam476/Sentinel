// SPDX-License-Identifier: Apache-2.0

package scripts

import "os/exec"

// ownGroup leaves Windows' default: stopping a script ends its process.
// Processes the script started and left running in the background aren't
// tracked; a job object would track them (a known limit, ADR 0024).
func ownGroup(*exec.Cmd) {}

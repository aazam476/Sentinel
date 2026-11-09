// SPDX-License-Identifier: Apache-2.0

// Command brokenagent is a deliberately broken agent build for the
// self-update end-to-end test (SEC-32): signed like a real release, it fails
// as soon as it starts, so the agent must refuse to keep it.
package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "brokenagent: this build is broken on purpose")
	os.Exit(2)
}

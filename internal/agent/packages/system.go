// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aazam476/sentinel/internal/agent/command"
)

// System is this machine, for New.
func System(goos string, runner command.Runner) Host {
	return Host{GOOS: goos, Runner: runner, Find: find, Exists: exists, Owner: owner}
}

// find returns a program on the PATH, or the newest match of a pattern
// with wildcards (winget's folder has its version in it).
func find(name string) string {
	if strings.Contains(name, "*") {
		matches, err := filepath.Glob(name)
		if err != nil || len(matches) == 0 {
			return ""
		}
		return matches[len(matches)-1]
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

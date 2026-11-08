// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths are where the agent keeps its files on this machine (§11.2).
type Paths struct {
	// StateDir holds the key and state.json; only root or SYSTEM may read it.
	StateDir string
	// ConfigDir holds the policy file, which only root or SYSTEM may change.
	ConfigDir string
}

// DefaultPaths returns the standard locations for the operating system
// goos ("linux", "darwin", "windows", ...). On Windows, programData is the
// value of %ProgramData%; it is ignored elsewhere.
func DefaultPaths(goos, programData string) Paths {
	switch goos {
	case "windows":
		if programData == "" {
			programData = `C:\ProgramData`
		}
		base := programData + `\Sentinel`
		return Paths{StateDir: base + `\state`, ConfigDir: base + `\etc`}
	case "darwin":
		return Paths{StateDir: "/Library/Application Support/Sentinel", ConfigDir: "/Library/Sentinel/etc"}
	default:
		return Paths{StateDir: "/var/lib/sentinel-agent", ConfigDir: "/etc/sentinel"}
	}
}

// PathsForThisMachine is DefaultPaths for the running operating system.
func PathsForThisMachine() Paths {
	return DefaultPaths(runtime.GOOS, os.Getenv("ProgramData"))
}

// PolicyFile is the policy file's path.
func (p Paths) PolicyFile() string {
	return filepath.Join(p.ConfigDir, "policy.toml")
}

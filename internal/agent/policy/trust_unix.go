// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package policy

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// checkTrusted refuses a policy file that someone other than the agent's
// own user (root) could have written: they could switch capabilities back
// on. Windows relies on the access list of C:\ProgramData\Sentinel\etc.
func checkTrusted(path string, info fs.FileInfo) error {
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("policy file %s can be changed by other users (mode %v); run: chmod go-w %s",
			path, info.Mode().Perm(), path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("policy file %s must belong to the user the agent runs as (uid %d), not uid %d",
			path, os.Geteuid(), stat.Uid)
	}
	return nil
}

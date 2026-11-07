// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package localaccess

import (
	"fmt"
	"os"
	"syscall"
)

// Check returns nil when the current process runs as root or as the owner
// of dataDir, and ErrNotAllowed otherwise.
func Check(dataDir string) error {
	return check(dataDir, os.Geteuid())
}

func check(dataDir string, effectiveUserID int) error {
	info, err := os.Stat(dataDir)
	if err != nil {
		return fmt.Errorf("check data directory: %w", err)
	}
	if effectiveUserID == 0 {
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != effectiveUserID {
		return ErrNotAllowed
	}
	return nil
}

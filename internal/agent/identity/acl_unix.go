// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package identity

import (
	"fmt"
	"os"
)

// restrictToAdministrators makes dir usable by its owner only. The agent
// runs as root, so that means root only. MkdirAll doesn't change the mode
// of a directory that already exists, so this sets it explicitly.
func restrictToAdministrators(dir string) error {
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: a directory needs its execute bit; 0700 is owner-only
		return fmt.Errorf("set permissions: %w", err)
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0

//go:build windows

package localaccess

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Check returns nil when the current process can write the database in
// dataDir, and ErrNotAllowed otherwise. Windows has no file owner check
// like Unix; instead the installer gives the data directory an access list
// that only lets SYSTEM and Administrators write it, so being able to write
// is the permission.
// https://learn.microsoft.com/en-us/windows/win32/secauthz/access-control-lists
func Check(dataDir string) error {
	if _, err := os.Stat(dataDir); err != nil {
		return fmt.Errorf("check data directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dataDir, "sentinel.db"), os.O_RDWR, 0)
	if errors.Is(err, fs.ErrPermission) {
		return ErrNotAllowed
	}
	if err != nil {
		return fmt.Errorf("check database: %w", err)
	}
	return file.Close()
}

// SPDX-License-Identifier: Apache-2.0

//go:build windows

package policy

import "io/fs"

// checkTrusted has nothing to check on Windows: the installer gives
// C:\ProgramData\Sentinel\etc an access list that only SYSTEM and
// Administrators can write through.
func checkTrusted(string, fs.FileInfo) error {
	return nil
}

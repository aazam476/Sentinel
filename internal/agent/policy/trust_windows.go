// SPDX-License-Identifier: Apache-2.0

//go:build windows

package policy

import (
	"fmt"
	"io/fs"

	"golang.org/x/sys/windows"
)

// checkTrusted refuses a policy file that anyone but SYSTEM,
// Administrators or TrustedInstaller owns or could change (SEC-22): the
// installer secures C:\ProgramData\Sentinel\etc, but a standard user who
// made the folder or file first would still own it.
func checkTrusted(path string, _ fs.FileInfo) error {
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("check who can change policy file %s: %w", path, err)
	}
	return checkSDDL(path, descriptor.String())
}

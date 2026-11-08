// SPDX-License-Identifier: Apache-2.0

//go:build windows

package identity

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// stateDirSDDL is the access list for the state directory, in Windows'
// SDDL notation:
//
//	D:P             a protected DACL: nothing is inherited from the parent
//	                folder (C:\ProgramData lets every user read by default)
//	(A;OICI;FA;;;SY) allow full access to SYSTEM, the agent's account,
//	                 inherited by files and subfolders
//	(A;OICI;FA;;;BA) and to the Administrators group
const stateDirSDDL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// restrictToAdministrators replaces dir's access list so only SYSTEM and
// Administrators can use it. Windows has no Unix modes; the 0700 that
// os.MkdirAll is given means nothing there.
func restrictToAdministrators(dir string) error {
	descriptor, err := windows.SecurityDescriptorFromString(stateDirSDDL)
	if err != nil {
		return fmt.Errorf("build access list: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read access list: %w", err)
	}
	err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("set access list: %w", err)
	}
	return nil
}

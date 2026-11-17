// SPDX-License-Identifier: Apache-2.0

//go:build windows

package identity

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// stateDirSDDL is the security for the state directory and everything in
// it, in Windows' SDDL notation:
//
//	O:BA             owned by the Administrators group
//	D:P              a protected DACL: nothing is inherited from the parent
//	                 folder (C:\ProgramData lets every user read by default)
//	(A;OICI;FA;;;SY) allow full access to SYSTEM, the agent's account,
//	                 inherited by files and subfolders
//	(A;OICI;FA;;;BA) and to the Administrators group
const stateDirSDDL = "O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// restrictToAdministrators makes dir and everything in it owned by
// Administrators and usable by SYSTEM and Administrators only (SEC-20).
// Setting the owner matters: a standard user who made the folder before
// the agent was installed would otherwise own it, and an owner can always
// change the access list back. Links and junctions are refused rather
// than followed. Windows has no Unix modes; the 0700 that os.MkdirAll is
// given means nothing there.
func restrictToAdministrators(dir string) error {
	descriptor, err := windows.SecurityDescriptorFromString(stateDirSDDL)
	if err != nil {
		return fmt.Errorf("build access list: %w", err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("read owner: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read access list: %w", err)
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("check %s: %w", path, err)
		}
		if err := refuseReparsePoint(path); err != nil {
			return err
		}
		err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
			windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			owner, nil, dacl, nil)
		if err != nil {
			return fmt.Errorf("secure %s: %w", path, err)
		}
		return nil
	})
}

// refuseReparsePoint refuses a symbolic link, junction or other reparse
// point: securing it would secure wherever it points instead.
func refuseReparsePoint(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("check %s: %w", path, err)
	}
	attributes, err := windows.GetFileAttributes(name)
	if err != nil {
		return fmt.Errorf("check %s: %w", path, err)
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("%s is a link or junction; remove it and start the agent again", path)
	}
	return nil
}

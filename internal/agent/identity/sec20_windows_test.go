// SPDX-License-Identifier: Apache-2.0

//go:build windows

package identity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSEC20_OnlySystemAndAdministratorsCanUseTheStateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	openStore(t, dir)
	descriptor, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	sddl := descriptor.String()
	// Protected (nothing inherited from the parent), and only SYSTEM (SY)
	// and Administrators (BA) are granted access.
	if !strings.Contains(sddl, "D:P") || strings.Count(sddl, "(A;") != 2 ||
		!strings.Contains(sddl, ";;;SY)") || !strings.Contains(sddl, ";;;BA)") {
		t.Errorf("access list = %s", sddl)
	}
	// Owned by Administrators, so whoever made the folder first can't
	// change the access list back.
	if !strings.HasPrefix(sddl, "O:BA") {
		t.Errorf("owner = %s, want Administrators", sddl)
	}
}

func TestSEC20_FilesLeftInTheStateDirectoryAreSecuredToo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(dir, "agent-key")
	if err := os.WriteFile(planted, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// As a standard user could leave it: everyone may write.
	open, err := windows.SecurityDescriptorFromString("D:(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, _ := open.DACL()
	if err := windows.SetNamedSecurityInfo(planted, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	openStore(t, dir)
	descriptor, err := windows.GetNamedSecurityInfo(planted, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if sddl := descriptor.String(); strings.Contains(sddl, ";;;WD)") || !strings.HasPrefix(sddl, "O:BA") {
		t.Errorf("planted file = %s, want only SYSTEM and Administrators, owned by Administrators", sddl)
	}
}

// SPDX-License-Identifier: Apache-2.0

//go:build windows

package identity_test

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSEC20_OnlySystemAndAdministratorsCanUseTheStateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	openStore(t, dir)
	descriptor, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
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
}

// SPDX-License-Identifier: Apache-2.0

package tray

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// socketDirSecurity lets SYSTEM and administrators do anything in the
// socket's folder, and users signed in at the machine (Interactive, not
// network or service logons) read the folder and read and write the
// files in it, which is what connecting to a Unix socket needs. They
// can't create, delete or replace files there.
const socketDirSecurity = "D:P(A;OICI;GA;;;SY)(A;OICI;GA;;;BA)(A;;0x1200a9;;;IU)(A;OIIO;GRGW;;;IU)"

// Listen opens the tray socket at path: a Unix socket (Windows 10 1803
// and later), never a network port (SEC-26). Only users signed in at the
// machine can open it, by its folder's permissions.
func Listen(path string) (net.Listener, error) {
	if err := secureSocketDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("prepare the tray socket: %w", err)
	}
	if err := prepareSocketDir(path, 0o700); err != nil {
		return nil, fmt.Errorf("prepare the tray socket: %w", err)
	}
	return listenUnix(path)
}

// secureSocketDir makes the folder, or resets an existing one's
// permissions, to socketDirSecurity.
func secureSocketDir(dir string) error {
	descriptor, err := windows.SecurityDescriptorFromString(socketDirSecurity)
	if err != nil {
		return fmt.Errorf("build the socket folder's permissions: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err //nolint:wrapcheck // wrapped by Listen
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return fmt.Errorf("socket folder name: %w", err)
	}
	attributes := windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	err = windows.CreateDirectory(name, &attributes)
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return fmt.Errorf("make the socket folder: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read the socket folder's permissions: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("set the socket folder's permissions: %w", err)
	}
	return nil
}

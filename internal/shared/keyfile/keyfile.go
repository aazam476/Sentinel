// SPDX-License-Identifier: Apache-2.0

package keyfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
)

// LoadOrCreate returns the contents of the key file at path. If there is no
// file yet, it saves the bytes returned by create there, readable only by
// the owner, and returns them. name describes the key in error messages.
func LoadOrCreate(path, name string, create func() []byte) ([]byte, error) {
	key, err := Read(path, name)
	if errors.Is(err, fs.ErrNotExist) {
		key = create()
		err = Write(path, name, key)
	}
	if err != nil {
		return nil, err
	}
	return key, nil
}

// Read returns the contents of the key file at path, after checking that
// no other user can read it.
func Read(path, name string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s %s is readable by other users (mode %v); run: chmod 600 %s",
			name, path, info.Mode().Perm(), path)
	}
	key, err := os.ReadFile(path) //nolint:gosec // G304: the path is chosen by the operator or the installer
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return key, nil
}

// Write saves key to a new file at path that only the owner can read. It
// never overwrites an existing file, so a key can't be replaced by mistake.
func Write(path, name string, key []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: the path is chosen by the operator or the installer
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	_, writeErr := file.Write(key)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

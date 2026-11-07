// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// KeyFileName is the key's file name inside the data directory.
const KeyFileName = "secret.key"

// LoadOrCreateKey reads <dataDir>/secret.key, creating it with a new random
// key (readable only by the server's user) if it doesn't exist, and returns
// a Box using it.
//
// It refuses a key file that other users can read: anyone who can read the
// key can decrypt every stored secret.
func LoadOrCreateKey(dataDir string) (*Box, error) {
	path := filepath.Join(dataDir, KeyFileName)

	key, err := readKeyFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		key, err = createKeyFile(path)
	}
	if err != nil {
		return nil, err
	}
	return NewBox(key)
}

func readKeyFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read secret key: %w", err)
	}
	// Windows has no Unix permission bits; the data directory's ACL protects
	// the file there (set up by the installer).
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("secret key %s is readable by other users (mode %v); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}

	key, err := os.ReadFile(path) //nolint:gosec // G304: the path is <data dir>/secret.key, chosen by the operator
	if err != nil {
		return nil, fmt.Errorf("read secret key: %w", err)
	}
	return key, nil
}

func createKeyFile(path string) ([]byte, error) {
	key := make([]byte, KeySize)
	// crypto/rand.Read never returns an error: if the system's random source
	// fails, Go stops the program rather than hand out predictable bytes.
	_, _ = rand.Read(key)

	// O_EXCL: never overwrite a key that appeared in the meantime, because
	// that would make every secret encrypted with it unreadable.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: the path is <data dir>/secret.key
	if err != nil {
		return nil, fmt.Errorf("create secret key: %w", err)
	}
	_, writeErr := file.Write(key)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return nil, fmt.Errorf("write secret key: %w", err)
	}
	return key, nil
}

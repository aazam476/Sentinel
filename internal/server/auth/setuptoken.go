// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// setupTokenFileName is the setup token's file in the data directory.
const setupTokenFileName = "setup-token"

// setupTokenBytes is the setup token's randomness: 256 bits (SEC-01).
const setupTokenBytes = 32

// readOrCreateSetupToken returns the token in the setup-token file, creating
// the file (readable only by the server's user) with a new random token if it
// doesn't exist.
func readOrCreateSetupToken(path string) (string, error) {
	existing, err := os.ReadFile(path) //nolint:gosec // G304: the path is <data dir>/setup-token
	if err == nil {
		return strings.TrimSpace(string(existing)), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read setup token: %w", err)
	}

	raw := make([]byte, setupTokenBytes)
	// crypto/rand.Read never returns an error: if the system's random source
	// fails, Go stops the program rather than hand out predictable bytes.
	_, _ = rand.Read(raw)
	token := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write setup token: %w", err)
	}
	return token, nil
}

// setupTokenMatches reports whether candidate equals the token in the file.
// A missing file never matches, because setup is then closed.
func setupTokenMatches(path, candidate string) bool {
	stored, err := os.ReadFile(path) //nolint:gosec // G304: the path is <data dir>/setup-token
	if err != nil {
		return false
	}
	expected := strings.TrimSpace(string(stored))
	candidate = strings.ToUpper(strings.TrimSpace(candidate))
	return expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(candidate)) == 1
}

// removeSetupToken deletes the setup token file. A file that is already gone
// is fine.
func removeSetupToken(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove setup token: %w", err)
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// setupTokenFileName is where servers before v0.1.0-beta.1 kept the setup
// token, which setup no longer asks for (ADR 0037).
const setupTokenFileName = "setup-token"

// removeSetupToken deletes the setup token file. A file that is already gone
// is fine.
func removeSetupToken(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove setup token: %w", err)
	}
	return nil
}

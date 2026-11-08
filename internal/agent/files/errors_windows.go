// SPDX-License-Identifier: Apache-2.0

package files

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Windows reports these with its own error codes rather than POSIX ones.
func isNotEmpty(err error) bool  { return errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) }
func isNotFolder(err error) bool { return errors.Is(err, windows.ERROR_DIRECTORY) }

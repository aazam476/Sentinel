// SPDX-License-Identifier: Apache-2.0

package packages

import "errors"

// owner isn't needed on Windows: only Homebrew asks.
func owner(string) (string, error) {
	return "", errors.New("not used on Windows")
}

// SPDX-License-Identifier: Apache-2.0

package localaccess

import "errors"

// ErrNotAllowed means the current user may not change the server's data.
// It is never reassigned.
var ErrNotAllowed = errors.New("run this as root or as the user that owns the data directory (normally: sudo -u sentinel)")

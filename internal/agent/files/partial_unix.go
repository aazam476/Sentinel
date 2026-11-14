// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package files

import (
	"io/fs"
	"syscall"
)

// openWithoutFollowing makes open fail on a symbolic link instead of
// opening what it points to, and return at once on a named pipe instead of
// waiting for its other end.
const openWithoutFollowing = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

// hasOtherNames reports whether the file is also reachable by another
// name: a hard link, which would let an upload write into a file that
// lives somewhere else too.
func hasOtherNames(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}

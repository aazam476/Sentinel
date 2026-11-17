// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package identity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// restrictToAdministrators makes dir and everything in it belong to the
// agent's user (root) and usable by it only (SEC-20). A directory that
// someone else made first, or a link to one, would otherwise stay theirs:
// MkdirAll leaves an existing directory as it is. Links are refused rather
// than followed, and the changes go through os.Root, so nothing swapped in
// while they are made can lead them outside dir.
func restrictToAdministrators(dir string) error {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return errors.Join(fmt.Errorf("%s isn't a directory (a link or file?); remove it and start the agent again", dir), err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }() // read-only use
	uid, gid := os.Geteuid(), os.Getegid()
	return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("check %s in %s: %w", path, dir, err)
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("%s in %s is a link or special file; remove it and start the agent again", path, dir)
		}
		mode := os.FileMode(0o600)
		if entry.IsDir() {
			mode = 0o700
		}
		if err := errors.Join(root.Lchown(path, uid, gid), root.Chmod(path, mode)); err != nil {
			return fmt.Errorf("secure %s in %s: %w", path, dir, err)
		}
		return nil
	})
}

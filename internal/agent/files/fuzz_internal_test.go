// SPDX-License-Identifier: Apache-2.0

package files

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// FuzzGuard checks SEC-25 against arbitrary paths: whatever the guard
// accepts really is inside the allowed folder once links are followed.
func FuzzGuard(f *testing.F) {
	for _, seed := range []string{"a", "../secret/shadow", "escape/shadow", "a/../../secret", "./a//b", "escape", "..", "\x00"} {
		f.Add(seed)
	}
	root, err := filepath.EvalSymlinks(f.TempDir())
	if err != nil {
		f.Fatal(err)
	}
	allowed, secret := filepath.Join(root, "allowed"), filepath.Join(root, "secret")
	for _, folder := range []string{filepath.Join(allowed, "a", "b"), secret} {
		if err := os.MkdirAll(folder, 0o750); err != nil {
			f.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(secret, filepath.Join(allowed, "escape")); err != nil {
			f.Fatal(err)
		}
	}
	g := guard{allowed: []string{allowed}}
	f.Fuzz(func(t *testing.T, relative string) {
		path := allowed + string(filepath.Separator) + relative
		if cleaned, err := g.existing(path); err == nil {
			real, err := filepath.EvalSymlinks(cleaned)
			if err == nil && !within(real, allowed) {
				t.Errorf("accepted %q, which is really %q", path, real)
			}
		}
		if cleaned, err := g.newEntry(path); err == nil {
			parent, err := filepath.EvalSymlinks(filepath.Dir(cleaned))
			if err == nil && !within(filepath.Join(parent, filepath.Base(cleaned)), allowed) {
				t.Errorf("accepted new %q, which would be in %q", path, parent)
			}
		}
	})
}

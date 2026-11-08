// SPDX-License-Identifier: Apache-2.0

package files

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	srv := sep + "srv"
	cases := []struct {
		path, folder string
		want         bool
	}{
		{srv, srv, true},
		{srv + sep + "media", srv, true},
		{srv + "2", srv, false},
		{sep + "srvx" + sep + "a", srv, false},
		{sep, srv, false},
		{srv + sep + "a", sep, true},
	}
	for _, tc := range cases {
		t.Run(tc.path+" in "+tc.folder, func(t *testing.T) {
			if got := within(tc.path, tc.folder); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsStart(t *testing.T) {
	sep := string(filepath.Separator)
	g := guard{allowed: []string{sep + "srv"}}
	if !g.isStart(sep+"srv") || g.isStart(sep+"srv"+sep+"a") {
		t.Error("an allowed folder is a start; what is inside it isn't")
	}
	if root := filepath.VolumeName(sep) + sep; !g.isStart(root) {
		t.Errorf("%s is a root", root)
	}
}

func TestDescribe(t *testing.T) {
	other := errors.New("input/output error")
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"a missing file", fs.ErrNotExist, ErrNotFound},
		{"an existing one", fs.ErrExist, ErrExists},
		{"anything else, with the path", other, other},
	}
	for _, tc := range cases {
		t.Run("describes "+tc.name, func(t *testing.T) {
			if got := describe(tc.err, "/srv/a"); !errors.Is(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEntryType(t *testing.T) {
	cases := map[fs.FileMode]string{
		fs.ModeDir: "dir", 0: "file", fs.ModeSymlink: "symlink", fs.ModeNamedPipe: "other", fs.ModeDevice: "other",
	}
	for mode, want := range cases {
		if got := entryType(mode); got != want {
			t.Errorf("%v: got %s, want %s", mode, got, want)
		}
	}
}

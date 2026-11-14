// SPDX-License-Identifier: Apache-2.0

package files

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
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

func TestDescribeEntries(t *testing.T) {
	// Twenty files whose names alone come to 4,000 bytes.
	folder := t.TempDir()
	for i := range 20 {
		name := strings.Repeat(string(rune('a'+i)), 200)
		if err := os.WriteFile(filepath.Join(folder, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dirEntries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("describes every entry when they fit in one message", func(t *testing.T) {
		entries, truncated := describeEntries(folder, dirEntries, maxListBytes)
		if len(entries) != 20 || truncated {
			t.Errorf("got %d entries, truncated %v", len(entries), truncated)
		}
	})

	t.Run("leaves out what would make the listing too big to send, and says so", func(t *testing.T) {
		const budget = 4_000
		entries, truncated := describeEntries(folder, dirEntries, budget)
		if len(entries) == 0 || len(entries) >= 20 || !truncated {
			t.Fatalf("got %d entries, truncated %v", len(entries), truncated)
		}
		encoded, err := json.Marshal(protocol.FileListResultPayload{Entries: entries})
		if err != nil {
			t.Fatal(err)
		}
		if overhead := len(`{"path":"","parent":"","entries":[],"truncated":false}`); len(encoded) > budget+overhead {
			t.Errorf("the listing is %d bytes encoded, over the %d it may be", len(encoded), budget)
		}
	})

	t.Run("an empty folder is an empty list, not a missing one", func(t *testing.T) {
		if entries, truncated := describeEntries(t.TempDir(), nil, maxListBytes); entries == nil || len(entries) != 0 || truncated {
			t.Errorf("got %v, truncated %v", entries, truncated)
		}
	})
}

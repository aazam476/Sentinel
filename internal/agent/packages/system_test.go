// SPDX-License-Identifier: Apache-2.0

package packages_test

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/packages"
)

func TestSystem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("checks Unix file owners")
	}
	machine := packages.System(runtime.GOOS, command.Exec{})
	dir := t.TempDir()
	file := filepath.Join(dir, "winget-1.2", "winget.exe")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("finds programs on the PATH and by pattern", func(t *testing.T) {
		if machine.Find("sh") == "" || machine.Find("no-such-program-here") != "" {
			t.Error("Find() didn't find sh, or found a missing program")
		}
		if got := machine.Find(filepath.Join(dir, "winget-*", "winget.exe")); got != file {
			t.Errorf("Find(pattern) = %q", got)
		}
		if machine.Find(filepath.Join(dir, "none-*")) != "" || machine.Find("[*") != "" {
			t.Error("Find() matched a pattern with no file")
		}
	})

	t.Run("says whether files exist and who owns them", func(t *testing.T) {
		if !machine.Exists(file) || machine.Exists(filepath.Join(dir, "missing")) {
			t.Error("Exists() is wrong")
		}
		me, err := user.Current()
		if err != nil {
			t.Fatal(err)
		}
		if got, err := machine.Owner(file); err != nil || got != me.Username {
			t.Errorf("Owner() = %q, %v", got, err)
		}
		if _, err := machine.Owner(filepath.Join(dir, "missing")); err == nil {
			t.Error("Owner() of a missing file succeeded")
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package terminal

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginShell(t *testing.T) {
	passwd := filepath.Join(t.TempDir(), "passwd")
	content := "root:x:0:0:root:/root:/bin/bash\nnobody:x:65534:65534::/:/usr/sbin/nologin\nbroken line\nodd:x:7:7::/:relative\n"
	if err := os.WriteFile(passwd, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		path     string
		uid      int
		variable string
		want     string
	}{
		{"uses the account's shell from the passwd file", passwd, 0, "/bin/zsh", "/bin/bash"},
		{"falls back to $SHELL for an account not listed", passwd, 1000, "/bin/zsh", "/bin/zsh"},
		{"ignores a relative shell in either place", passwd, 7, "zsh", "/bin/sh"},
		{"falls back to /bin/sh without a passwd file", filepath.Join(t.TempDir(), "missing"), 0, "", "/bin/sh"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := loginShell(test.path, test.uid, test.variable); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

// TestPTY runs a real shell in a real pseudo-terminal: the code under test
// is the thin layer over the operating system.
func TestPTY(t *testing.T) {
	t.Run("runs a shell that echoes commands and output, resizes and ends", func(t *testing.T) {
		shell, err := startPTY("/bin/sh", 80, 24)
		if err != nil {
			t.Fatal(err)
		}
		if err := shell.Resize(100, 30); err != nil {
			t.Errorf("Resize: %v", err)
		}
		if _, err := shell.Write([]byte("echo sentinel-$((40 + 2))\nexit\n")); err != nil {
			t.Fatal(err)
		}
		output, err := io.ReadAll(shell)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(output), "sentinel-42") {
			t.Errorf("output = %q", output)
		}
		_ = shell.Close()
	})

	t.Run("reports a shell that can't be started", func(t *testing.T) {
		if _, err := startPTY(filepath.Join(t.TempDir(), "no-shell"), 80, 24); err == nil {
			t.Error("startPTY returned nil")
		}
	})

	t.Run("the starter starts this machine's login shell", func(t *testing.T) {
		shell, err := NewStarter()(80, 24)
		if err != nil {
			t.Fatal(err)
		}
		if err := shell.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
}

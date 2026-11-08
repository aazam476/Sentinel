// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/testutil/clitest"
)

func TestAgentCommandLine(t *testing.T) {
	binary := clitest.Build(t, "github.com/aazam476/sentinel/cmd/sentinel-agent")

	tests := []struct {
		name         string
		args         []string
		wantExitCode int
		wantStdout   string
		wantStderr   string
	}{
		{
			name:         "version prints the program name and version",
			args:         []string{"version"},
			wantExitCode: 0,
			wantStdout:   "sentinel-agent ",
		},
		{
			name:         "help prints usage to stdout",
			args:         []string{"help"},
			wantExitCode: 0,
			wantStdout:   "Usage:",
		},
		{
			name:         "--help prints usage to stdout",
			args:         []string{"--help"},
			wantExitCode: 0,
			wantStdout:   "Usage:",
		},
		{
			name:         "no command prints usage and exits with a usage error",
			args:         nil,
			wantExitCode: 2,
			wantStderr:   "Usage:",
		},
		{
			name:         "an unknown command is named in the error",
			args:         []string{"frobnicate"},
			wantExitCode: 2,
			wantStderr:   `unknown command "frobnicate"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := clitest.Run(t, binary, test.args...)

			if result.ExitCode != test.wantExitCode {
				t.Errorf("exit code = %d, want %d", result.ExitCode, test.wantExitCode)
			}
			if !strings.Contains(result.Stdout, test.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", result.Stdout, test.wantStdout)
			}
			if !strings.Contains(result.Stderr, test.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", result.Stderr, test.wantStderr)
			}
		})
	}
}

func TestAgentSetupCommands(t *testing.T) {
	binary := clitest.Build(t, "github.com/aazam476/sentinel/cmd/sentinel-agent")
	// dirs returns a state directory and the flags that put the agent's
	// files under a temporary directory.
	dirs := func(t *testing.T) (string, []string) {
		t.Helper()
		root := t.TempDir()
		state := filepath.Join(root, "state")
		return state, []string{"--state-dir", state, "--config-dir", filepath.Join(root, "etc")}
	}

	t.Run("install without the server's details explains what is missing", func(t *testing.T) {
		result := clitest.Run(t, binary, "install", "--server", "https://sentinel.example.com")
		if result.ExitCode != 2 || !strings.Contains(result.Stderr, "--token and --fingerprint are required") {
			t.Errorf("exit %d, stderr %q", result.ExitCode, result.Stderr)
		}
	})

	t.Run("an unknown flag is a usage error", func(t *testing.T) {
		for _, command := range []string{"install", "uninstall", "run"} {
			if result := clitest.Run(t, binary, command, "--frobnicate"); result.ExitCode != 2 {
				t.Errorf("%s: exit %d, want 2", command, result.ExitCode)
			}
		}
	})

	t.Run("install --dry-run describes the steps and changes nothing", func(t *testing.T) {
		state, paths := dirs(t)
		args := append([]string{"install", "--dry-run", "--server", "https://sentinel.example.com", "--token", "t",
			"--fingerprint", "SHA256:x"}, paths...)
		result := clitest.Run(t, binary, args...)
		if result.ExitCode != 0 || !strings.Contains(result.Stdout, "Would enroll with https://sentinel.example.com") ||
			!strings.Contains(result.Stdout, "Would install the Sentinel Agent service") {
			t.Errorf("exit %d, stdout %q, stderr %q", result.ExitCode, result.Stdout, result.Stderr)
		}
		if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
			t.Error("a dry run created the state directory")
		}
	})

	t.Run("uninstall --dry-run describes the steps", func(t *testing.T) {
		_, paths := dirs(t)
		result := clitest.Run(t, binary, append([]string{"uninstall", "--dry-run"}, paths...)...)
		if result.ExitCode != 0 || !strings.Contains(result.Stdout, "Would stop and remove") {
			t.Errorf("exit %d, stdout %q", result.ExitCode, result.Stdout)
		}
	})

	t.Run("enroll reports a server it can't reach", func(t *testing.T) {
		_, paths := dirs(t)
		args := append([]string{"enroll", "--server", "https://127.0.0.1:1", "--token", "t", "--fingerprint", "SHA256:x"}, paths...)
		if result := clitest.Run(t, binary, args...); result.ExitCode != 1 || !strings.Contains(result.Stderr, "sentinel-agent enroll:") {
			t.Errorf("exit %d, stderr %q", result.ExitCode, result.Stderr)
		}
	})

	t.Run("run refuses to start before enrollment", func(t *testing.T) {
		_, paths := dirs(t)
		result := clitest.Run(t, binary, append([]string{"run"}, paths...)...)
		if result.ExitCode != 1 || !strings.Contains(result.Stderr, "isn't enrolled yet") {
			t.Errorf("exit %d, stderr %q", result.ExitCode, result.Stderr)
		}
	})
}

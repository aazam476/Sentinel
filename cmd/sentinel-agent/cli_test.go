// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main_test

import (
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

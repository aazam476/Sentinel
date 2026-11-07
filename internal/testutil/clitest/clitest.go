// SPDX-License-Identifier: Apache-2.0

package clitest

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Result is what a finished command printed and how it exited.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Build compiles the Go package at packagePath (for example
// "github.com/aazam476/sentinel/cmd/sentinel-server") into a temporary folder
// and returns the binary's path. The folder is removed when the test ends.
func Build(t *testing.T, packagePath string) string {
	t.Helper()

	binaryPath := filepath.Join(t.TempDir(), filepath.Base(packagePath))
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	command := exec.CommandContext(t.Context(), "go", "build", "-o", binaryPath, packagePath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, output)
	}
	return binaryPath
}

// Run runs the binary with args, waits for it to exit and returns its output
// and exit code. It fails the test if the binary couldn't be started at all.
func Run(t *testing.T, binaryPath string, args ...string) Result {
	t.Helper()

	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(t.Context(), binaryPath, args...)
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("run %s: %v", binaryPath, err)
	}

	return Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: command.ProcessState.ExitCode(),
	}
}

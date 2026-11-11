// SPDX-License-Identifier: Apache-2.0

package tray_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

func TestSocketPathIsTheSystemsStandardPlace(t *testing.T) {
	tests := []struct {
		goos, programData, want string
		supported               bool
	}{
		{goos: "windows", programData: `D:\Data`, want: `D:\Data\Sentinel\tray\tray.sock`, supported: true},
		{goos: "windows", want: `C:\ProgramData\Sentinel\tray\tray.sock`, supported: true},
		{goos: "darwin", want: "/var/run/sentinel-agent/tray.sock", supported: true},
		{goos: "linux", want: "/run/sentinel-agent/tray.sock"},
	}
	for _, test := range tests {
		t.Run(test.goos+" "+test.programData, func(t *testing.T) {
			if got := tray.SocketPath(test.goos, test.programData); got != test.want {
				t.Errorf("SocketPath = %q, want %q", got, test.want)
			}
			if got := tray.Supported(test.goos); got != test.supported {
				t.Errorf("Supported = %v", got)
			}
		})
	}
	if tray.SocketPathForThisMachine() != tray.SocketPath(runtime.GOOS, os.Getenv("ProgramData")) {
		t.Error("SocketPathForThisMachine isn't this system's path")
	}
}

func TestListenReportsWhereItCant(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tray.Listen(filepath.Join(file, "tray.sock")); err == nil {
		t.Error("Listen under a file succeeded")
	}
	if _, err := tray.Dial(t.Context(), filepath.Join(t.TempDir(), "nothing.sock")); err == nil {
		t.Error("Dial to no socket succeeded")
	}
}

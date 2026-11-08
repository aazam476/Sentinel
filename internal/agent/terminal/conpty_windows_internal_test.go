// SPDX-License-Identifier: Apache-2.0

//go:build windows

package terminal

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestConPTY runs real PowerShell in a real pseudo-console.
func TestConPTY(t *testing.T) {
	t.Run("runs PowerShell that answers a command, resizes and ends", func(t *testing.T) {
		shell, err := NewStarter()(120, 30)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = shell.Close() }() // the test is over either way
		if err := shell.Resize(100, 40); err != nil {
			t.Errorf("Resize: %v", err)
		}
		chunks := make(chan []byte, 64)
		go func() {
			defer close(chunks)
			for {
				buffer := make([]byte, 4096)
				n, err := shell.Read(buffer)
				if n > 0 {
					chunks <- buffer[:n]
				}
				if err != nil {
					return
				}
			}
		}()
		var seen bytes.Buffer
		// PowerShell drops what is typed before its prompt is up, as a
		// user's early keystrokes would be, so wait for the prompt first.
		waitForOutput(t, chunks, &seen, "PS ")
		if _, err := shell.Write([]byte("Write-Output (40 + 2)\r")); err != nil {
			t.Fatal(err)
		}
		waitForOutput(t, chunks, &seen, "42")
	})

	t.Run("reports a program that can't be started", func(t *testing.T) {
		if _, err := startConPTY(`C:\no\such\program.exe`, 80, 24); err == nil {
			t.Error("startConPTY returned nil")
		}
	})
}

// waitForOutput reads chunks into seen until it contains want. PowerShell's
// first start on a fresh machine can be slow, hence the long limit.
func waitForOutput(t *testing.T, chunks <-chan []byte, seen *bytes.Buffer, want string) {
	t.Helper()
	deadline := time.After(90 * time.Second)
	for !strings.Contains(seen.String(), want) {
		select {
		case chunk, open := <-chunks:
			if !open {
				t.Fatalf("the shell ended before printing %q; output: %q", want, seen.String())
			}
			seen.Write(chunk)
		case <-deadline:
			t.Fatalf("no %q from PowerShell; output so far: %q", want, seen.String())
		}
	}
}

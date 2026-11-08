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
		if _, err := shell.Write([]byte("Write-Output (40 + 2)\r")); err != nil {
			t.Fatal(err)
		}
		output := make(chan string, 1)
		go func() {
			var seen bytes.Buffer
			buffer := make([]byte, 4096)
			for !strings.Contains(seen.String(), "42") {
				n, err := shell.Read(buffer)
				seen.Write(buffer[:n])
				if err != nil {
					break
				}
			}
			output <- seen.String()
		}()
		select {
		case got := <-output:
			if !strings.Contains(got, "42") {
				t.Errorf("output = %q", got)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("PowerShell never answered")
		}
	})

	t.Run("reports a program that can't be started", func(t *testing.T) {
		if _, err := startConPTY(`C:\no\such\program.exe`, 80, 24); err == nil {
			t.Error("startConPTY returned nil")
		}
	})
}

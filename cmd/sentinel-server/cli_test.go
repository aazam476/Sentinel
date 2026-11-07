// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main_test

import (
	"bufio"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/testutil/clitest"
)

const serverPackage = "github.com/aazam476/sentinel/cmd/sentinel-server"

func TestServerCommandLine(t *testing.T) {
	binary := clitest.Build(t, serverPackage)

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
			wantStdout:   "sentinel-server ",
		},
		{
			name:         "-help prints usage and the listen flag",
			args:         []string{"-help"},
			wantExitCode: 0,
			wantStderr:   "-listen",
		},
		{
			name:         "an unknown flag exits with a usage error",
			args:         []string{"--frobnicate"},
			wantExitCode: 2,
			wantStderr:   "frobnicate",
		},
		{
			name:         "an unknown command is named in the error",
			args:         []string{"frobnicate"},
			wantExitCode: 2,
			wantStderr:   `unknown command "frobnicate"`,
		},
		{
			name:         "refuses to listen on every interface without TLS",
			args:         []string{"--listen", "0.0.0.0:0"},
			wantExitCode: 1,
			wantStderr:   "only loopback addresses are allowed",
		},
		{
			name:         "refuses an empty listen host",
			args:         []string{"--listen", ":0"},
			wantExitCode: 1,
			wantStderr:   "host is empty",
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

func TestServerServesWebUI(t *testing.T) {
	binary := clitest.Build(t, serverPackage)
	command := exec.CommandContext(t.Context(), binary, "--listen", "127.0.0.1:0")
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	// t.Context is canceled when the test ends, which kills the server.
	t.Cleanup(func() { _ = command.Wait() }) // the exit error after being killed is expected

	address := waitForListeningAddress(t, stderr)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = response.Body.Close() }() // nothing to do if closing fails in a test
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// make test-integration builds the web UI first, so it must be embedded.
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200; body: %s", response.StatusCode, body)
	}
	if !strings.Contains(string(body), `<div id="root">`) {
		t.Errorf("GET / body does not contain the app root element: %s", body)
	}
}

// waitForListeningAddress reads the server's log until it reports the
// address it is listening on. The server logs this line once the socket is
// open, so the address is ready to accept connections when it returns.
func waitForListeningAddress(t *testing.T, logOutput io.Reader) string {
	t.Helper()

	scanner := bufio.NewScanner(logOutput)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `msg="server listening"`) {
			continue
		}
		for _, field := range strings.Fields(line) {
			if address, found := strings.CutPrefix(field, "address="); found {
				// Keep draining the log so the server never blocks writing to it.
				go func() { _, _ = io.Copy(io.Discard, logOutput) }() // ends when the server exits
				return address
			}
		}
	}
	t.Fatalf("server exited before logging its listening address (scan error: %v)", scanner.Err())
	return ""
}

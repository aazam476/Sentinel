// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main_test

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/testutil/clitest"
)

const serverPackage = "github.com/aazam476/sentinel/cmd/sentinel-server"

func TestServerCommandLine(t *testing.T) {
	binary := clitest.Build(t, serverPackage)
	plainHTTPEverywhere := writeFile(t, "server.toml", "[tls]\nmode = \"off\"\n")
	typo := writeFile(t, "typo.toml", "lisen = \":7443\"\n")

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
			name:         "-help lists the flags",
			args:         []string{"-help"},
			wantExitCode: 0,
			wantStderr:   "-config",
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
			name:         "SEC-10 refuses plain HTTP on every interface",
			args:         []string{"--config", plainHTTPEverywhere, "--listen", ":0", "--data-dir", t.TempDir()},
			wantExitCode: 1,
			wantStderr:   "plain HTTP is only allowed",
		},
		{
			name:         "a typo in the config file is reported",
			args:         []string{"--config", typo},
			wantExitCode: 1,
			wantStderr:   "lisen",
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

func TestSEC10_ServerUsesTLSByDefault(t *testing.T) {
	binary := clitest.Build(t, serverPackage)
	dataDir := t.TempDir()
	address := startServer(t, binary, "--config", filepath.Join(dataDir, "missing.toml"),
		"--data-dir", dataDir, "--listen", "127.0.0.1:0")
	client := clientTrusting(t, filepath.Join(dataDir, "tls", "self-signed.crt"))

	t.Run("serves the web UI over HTTPS with its self-signed certificate", func(t *testing.T) {
		response := get(t, client, "https://"+address+"/")
		if response.status != http.StatusOK || !strings.Contains(response.body, `<div id="root">`) {
			t.Errorf("GET / = %d %q, want 200 with the app root element", response.status, response.body)
		}
	})

	t.Run("answers the health check", func(t *testing.T) {
		response := get(t, client, "https://"+address+"/healthz")
		if response.status != http.StatusOK || response.body != "ok" {
			t.Errorf("GET /healthz = %d %q, want 200 ok", response.status, response.body)
		}
	})

	t.Run("SEC-11 sends security headers", func(t *testing.T) {
		response := get(t, client, "https://"+address+"/")
		if response.header.Get("Content-Security-Policy") == "" || response.header.Get("X-Frame-Options") != "DENY" {
			t.Errorf("headers = %v, want a CSP and X-Frame-Options: DENY", response.header)
		}
	})

	t.Run("SEC-12 refuses a state-changing request from another site", func(t *testing.T) {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://"+address+"/api/anything", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Origin", "https://evil.example.com")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		_ = response.Body.Close() // only the status matters
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", response.StatusCode)
		}
	})
}

func TestServerServesPlainHTTPOnLoopback(t *testing.T) {
	binary := clitest.Build(t, serverPackage)
	configPath := writeFile(t, "server.toml", "[tls]\nmode = \"off\"\n")
	address := startServer(t, binary, "--config", configPath, "--data-dir", t.TempDir(), "--listen", "127.0.0.1:0")

	response := get(t, http.DefaultClient, "http://"+address+"/healthz")

	if response.status != http.StatusOK || response.body != "ok" {
		t.Errorf("GET /healthz = %d %q, want 200 ok", response.status, response.body)
	}
}

// startServer runs the server binary until the test ends and returns the
// address it listens on.
func startServer(t *testing.T, binary string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), binary, args...)
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	// t.Context is canceled when the test ends, which kills the server.
	t.Cleanup(func() { _ = command.Wait() }) // the exit error after being killed is expected

	return waitForListeningAddress(t, stderr)
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

// clientTrusting returns an HTTP client that trusts only the certificate in
// certificatePath, proving the server serves the certificate it saved.
func clientTrusting(t *testing.T, certificatePath string) *http.Client {
	t.Helper()
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		t.Fatalf("read server certificate: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("server certificate is not valid PEM")
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
}

// getResult is what a GET request returned, read in full.
type getResult struct {
	status int
	header http.Header
	body   string
}

func get(t *testing.T, client *http.Client, url string) getResult {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = response.Body.Close() }() // nothing to do if closing fails in a test
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return getResult{status: response.StatusCode, header: response.Header, body: string(body)}
}

func writeFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

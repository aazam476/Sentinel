// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runIn(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

const developmentKey = "../../testdata/release/development.key"

func TestSignAndVerify(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "sentinel-agent_v1.0.0_linux_amd64")
	if err := os.WriteFile(artifact, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("signs an artifact that the compiled-in development key then accepts", func(t *testing.T) {
		code, out, stderr := runIn(t, "sign", "--key", developmentKey, "--name", "sentinel-agent", "--version", "v1.0.0", "--os", "linux", "--arch", "amd64", artifact)
		if code != exitOK || !strings.Contains(out, "signed") {
			t.Fatalf("sign = %d %q %q", code, out, stderr)
		}
		code, out, _ = runIn(t, "verify", "--name", "sentinel-agent", "--os", "linux", artifact)
		if code != exitOK || !strings.Contains(out, "good signature for sentinel-agent v1.0.0 linux/amd64") {
			t.Errorf("verify = %d %q", code, out)
		}
		if code, _, stderr := runIn(t, "verify", "--os", "windows", artifact); code != exitFailure || !strings.Contains(stderr, "isn't the release") {
			t.Errorf("verify for another platform = %d %q", code, stderr)
		}
	})

	t.Run("reads the key from the environment", func(t *testing.T) {
		key, err := os.ReadFile(developmentKey)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv(keyVariable, string(key))
		if code, _, stderr := runIn(t, "sign", "--name", "x", "--version", "v1", artifact); code != exitOK {
			t.Errorf("sign = %d %q", code, stderr)
		}
	})

	t.Run("lists checksums by file name", func(t *testing.T) {
		code, out, _ := runIn(t, "sums", artifact)
		// sha256("binary")
		want := "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd  sentinel-agent_v1.0.0_linux_amd64\n"
		if code != exitOK || out != want {
			t.Errorf("sums = %d %q", code, out)
		}
	})

	t.Run("makes a key pair", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "release.key")
		code, out, _ := runIn(t, "keygen", file)
		if code != exitOK || !strings.Contains(out, "production.pub") {
			t.Fatalf("keygen = %d %q", code, out)
		}
		if info, err := os.Stat(file); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
			t.Errorf("the private key is %v, %v", info, err)
		}
		if code, _, _ := runIn(t, "keygen", file); code != exitFailure {
			t.Error("keygen overwrote a key")
		}
	})
}

func TestSBOM(t *testing.T) {
	binary, err := os.Executable() // this test binary, which has Go build information
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1762646400")
	code, out, stderr := runIn(t, "sbom", "--name", "sentinel-server", "--version", "v1.0.0", "--npm-lock", "../../web/package-lock.json", binary)
	if code != exitOK {
		t.Fatalf("sbom = %d %q", code, stderr)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(out), &document); err != nil || document["spdxVersion"] != "SPDX-2.3" {
		t.Errorf("got %v", err)
	}
	for _, want := range []string{"pkg:npm/react@", "2025-11-09T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("the SBOM has no %q", want)
		}
	}
	if code, _, _ := runIn(t, "sbom", "--name", "x", "--version", "v1", "--npm-lock", "missing.json", binary); code != exitFailure {
		t.Error("a missing lockfile was accepted")
	}
	notGo := filepath.Join(t.TempDir(), "script.sh")
	_ = os.WriteFile(notGo, []byte("#!/bin/sh\n"), 0o600)
	if code, _, _ := runIn(t, "sbom", "--name", "x", "--version", "v1", notGo); code != exitFailure {
		t.Error("a file that isn't a Go binary was accepted")
	}
}

func TestRefusals(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"no command", nil, exitUsageError},
		{"an unknown command", []string{"publish"}, exitUsageError},
		{"no file", []string{"sign", "--key", developmentKey}, exitUsageError},
		{"an unknown flag", []string{"verify", "--everything", "x"}, exitUsageError},
		{"signing with no key", []string{"sign", "--name", "x", "--version", "v1", "x"}, exitUsageError},
		{"a key file that isn't there", []string{"sign", "--key", missing, "x"}, exitFailure},
		{"signing a file that isn't there", []string{"sign", "--key", developmentKey, "--name", "x", "--version", "v1", missing}, exitFailure},
		{"a name that can't be signed", []string{"sign", "--key", developmentKey, "--name", "a b", "--version", "v1", developmentKey}, exitFailure},
		{"verifying with no signature", []string{"verify", developmentKey}, exitFailure},
		{"summing a file that isn't there", []string{"sums", missing}, exitFailure},
		{"an SBOM of a file that isn't there", []string{"sbom", missing}, exitFailure},
		{"a key file in a folder that isn't there", []string{"keygen", filepath.Join(missing, "key")}, exitFailure},
	}
	t.Setenv(keyVariable, "")
	for _, tc := range cases {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			if code, _, _ := runIn(t, tc.args...); code != tc.code {
				t.Errorf("exit code %d, want %d", code, tc.code)
			}
		})
	}
	t.Run("refuses to vouch for a development build", func(t *testing.T) {
		if code, _, stderr := runIn(t, "check-key"); code != exitFailure || !strings.Contains(stderr, "keygen") {
			t.Errorf("check-key = %d %q", code, stderr)
		}
	})
}

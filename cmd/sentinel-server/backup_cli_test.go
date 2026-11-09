// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/testutil/clitest"
)

func writeFile600(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBackupCommands(t *testing.T) {
	binary := clitest.Build(t, serverPackage)
	missingConfig := filepath.Join(t.TempDir(), "missing.toml")
	phrase := writeFile600(t, filepath.Join(t.TempDir(), "passphrase"), "a long backup passphrase\n")

	t.Run("backs up a server and restores it elsewhere, with its users and keys", func(t *testing.T) {
		dataDir := t.TempDir()
		setUpAdmin(t, dataDir)
		config := writeFile600(t, filepath.Join(t.TempDir(), "server.toml"), "data_dir = \""+filepath.ToSlash(dataDir)+"\"\n")
		file := filepath.Join(t.TempDir(), "sentinel.backup")
		result := clitest.Run(t, binary, "backup", "--config", config, "--passphrase-file", phrase, file)
		if result.ExitCode != 0 || !strings.Contains(result.Stdout, "Backed up to") {
			t.Fatalf("backup = %d %q %q", result.ExitCode, result.Stdout, result.Stderr)
		}
		if info, err := os.Stat(file); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("the backup is %v, %v; want readable only by its owner", info, err)
		}
		if leftovers, _ := filepath.Glob(filepath.Join(dataDir, "*.tmp")); len(leftovers) != 0 {
			t.Errorf("left %v behind", leftovers)
		}

		restored := t.TempDir()
		newConfig := filepath.Join(t.TempDir(), "etc", "server.toml")
		result = clitest.Run(t, binary, "restore", "--config", newConfig, "--data-dir", restored, "--passphrase-file", phrase, file)
		if result.ExitCode != 0 {
			t.Fatalf("restore = %d %q", result.ExitCode, result.Stderr)
		}
		for _, name := range []string{"secret.key", "server-key"} {
			original, _ := os.ReadFile(filepath.Join(dataDir, name))
			copied, err := os.ReadFile(filepath.Join(restored, name))
			if err != nil || len(original) == 0 || !bytes.Equal(original, copied) {
				t.Errorf("%s wasn't restored: %v", name, err)
			}
		}
		if content, err := os.ReadFile(newConfig); err != nil || !strings.Contains(string(content), "data_dir") {
			t.Errorf("config = %q, %v", content, err)
		}
		server := openApp(t, restored)
		login, err := server.Auth.Login(t.Context(), auth.LoginInput{Username: "admin", Password: "correct horse battery staple"}, auth.RequestInfo{IP: "127.0.0.1"})
		if err != nil || login.NextStep == "" {
			t.Errorf("signing in on the restored server = %+v, %v", login, err)
		}
		if err := server.Close(); err != nil {
			t.Fatal(err)
		}

		// Restoring over a server needs --force, and keeps an existing config.
		result = clitest.Run(t, binary, "restore", "--config", newConfig, "--data-dir", restored, "--passphrase-file", phrase, file)
		if result.ExitCode != 1 || !strings.Contains(result.Stderr, "--force") {
			t.Errorf("restore over a database = %d %q, want a refusal naming --force", result.ExitCode, result.Stderr)
		}
		result = clitest.Run(t, binary, "restore", "--config", config, "--data-dir", restored, "--passphrase-file", phrase, file)
		if result.ExitCode != 1 {
			t.Errorf("restore over a database = %d", result.ExitCode)
		}
		empty := t.TempDir()
		result = clitest.Run(t, binary, "restore", "--config", config, "--data-dir", empty, "--passphrase-file", phrase, file)
		if result.ExitCode != 0 || !strings.Contains(result.Stdout, "was kept") {
			t.Errorf("restore with a config present = %d %q", result.ExitCode, result.Stdout)
		}
		result = clitest.Run(t, binary, "restore", "--force", "--config", newConfig, "--data-dir", restored, "--passphrase-file", phrase, file)
		if result.ExitCode != 0 {
			t.Errorf("restore --force = %d %q", result.ExitCode, result.Stderr)
		}
	})

	t.Run("refuses a wrong passphrase and changes nothing", func(t *testing.T) {
		dataDir := t.TempDir()
		setUpAdmin(t, dataDir)
		file := filepath.Join(t.TempDir(), "sentinel.backup")
		if result := clitest.Run(t, binary, "backup", "--config", missingConfig, "--data-dir", dataDir, "--passphrase-file", phrase, file); result.ExitCode != 0 {
			t.Fatalf("backup = %d %q", result.ExitCode, result.Stderr)
		}
		wrong := writeFile600(t, filepath.Join(t.TempDir(), "wrong"), "not the passphrase at all")
		target := t.TempDir()
		result := clitest.Run(t, binary, "restore", "--config", missingConfig, "--data-dir", target, "--passphrase-file", wrong, file)
		if result.ExitCode != 1 || !strings.Contains(result.Stderr, "passphrase is wrong") {
			t.Errorf("restore = %d %q", result.ExitCode, result.Stderr)
		}
		if entries, _ := os.ReadDir(target); len(entries) != 0 {
			t.Errorf("a failed restore left %v", entries)
		}
	})

	t.Run("refuses to overwrite a file, a short passphrase, and a data directory without a database", func(t *testing.T) {
		dataDir := t.TempDir()
		setUpAdmin(t, dataDir)
		existing := writeFile600(t, filepath.Join(t.TempDir(), "existing"), "keep me")
		if result := clitest.Run(t, binary, "backup", "--config", missingConfig, "--data-dir", dataDir, "--passphrase-file", phrase, existing); result.ExitCode != 1 {
			t.Errorf("backup over a file = %d", result.ExitCode)
		}
		if content, _ := os.ReadFile(existing); string(content) != "keep me" {
			t.Error("the existing file was changed")
		}
		short := writeFile600(t, filepath.Join(t.TempDir(), "short"), "short")
		result := clitest.Run(t, binary, "backup", "--config", missingConfig, "--data-dir", dataDir, "--passphrase-file", short, filepath.Join(t.TempDir(), "b"))
		if result.ExitCode != 1 || !strings.Contains(result.Stderr, "at least 12") {
			t.Errorf("backup with a short passphrase = %d %q", result.ExitCode, result.Stderr)
		}
		result = clitest.Run(t, binary, "backup", "--config", missingConfig, "--data-dir", t.TempDir(), "--passphrase-file", phrase, filepath.Join(t.TempDir(), "b"))
		if result.ExitCode != 1 || !strings.Contains(result.Stderr, "no database") {
			t.Errorf("backup of nothing = %d %q", result.ExitCode, result.Stderr)
		}
	})

	t.Run("usage errors", func(t *testing.T) {
		for _, args := range [][]string{{"backup"}, {"restore"}, {"backup", "a", "b"}, {"restore", "--everything", "x"}} {
			if result := clitest.Run(t, binary, args...); result.ExitCode != 2 {
				t.Errorf("%v = %d, want 2", args, result.ExitCode)
			}
		}
	})
}

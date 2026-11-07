// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main_test

import (
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/aazam476/sentinel/internal/server/app"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/shared/clock"
	"github.com/aazam476/sentinel/internal/testutil/clitest"
)

func TestSEC08_ResetAdminCommand(t *testing.T) {
	binary := clitest.Build(t, serverPackage)
	missingConfig := filepath.Join(t.TempDir(), "missing.toml")

	t.Run("gives the admin a new password and requires 2FA enrollment again", func(t *testing.T) {
		dataDir := t.TempDir()
		setUpAdmin(t, dataDir)

		result := clitest.Run(t, binary, "reset-admin", "--config", missingConfig, "--data-dir", dataDir)

		if result.ExitCode != 0 {
			t.Fatalf("reset-admin = %d %q", result.ExitCode, result.Stderr)
		}
		match := regexp.MustCompile(`New password: (\S+)`).FindStringSubmatch(result.Stdout)
		if match == nil {
			t.Fatalf("stdout = %q, want the new password", result.Stdout)
		}
		server := openApp(t, dataDir)
		login, err := server.Auth.Login(t.Context(), auth.LoginInput{Username: "admin", Password: match[1]}, auth.RequestInfo{IP: "127.0.0.1"})
		if err != nil || login.NextStep != auth.NextStepEnrollSecondFactor {
			t.Errorf("Login with the new password = %+v, %v; want enrollment next", login, err)
		}
	})

	t.Run("refuses a data directory without a database", func(t *testing.T) {
		result := clitest.Run(t, binary, "reset-admin", "--config", missingConfig, "--data-dir", t.TempDir())
		if result.ExitCode != 1 || !strings.Contains(result.Stderr, "no database") {
			t.Errorf("reset-admin = %d %q, want exit 1 and 'no database'", result.ExitCode, result.Stderr)
		}
	})

	t.Run("reports an unknown admin", func(t *testing.T) {
		dataDir := t.TempDir()
		setUpAdmin(t, dataDir)
		result := clitest.Run(t, binary, "reset-admin", "--config", missingConfig, "--data-dir", dataDir, "--username", "nobody")
		if result.ExitCode != 1 {
			t.Errorf("reset-admin --username nobody exit code = %d, want 1", result.ExitCode)
		}
	})

	t.Run("an unknown flag is a usage error", func(t *testing.T) {
		if result := clitest.Run(t, binary, "reset-admin", "--everyone"); result.ExitCode != 2 {
			t.Errorf("exit code = %d, want 2", result.ExitCode)
		}
	})
}

// setUpAdmin creates the first admin in dataDir through the auth service,
// as the setup page would.
func setUpAdmin(t *testing.T, dataDir string) {
	t.Helper()
	server := openApp(t, dataDir)
	token, err := server.Auth.PrepareSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	client := auth.RequestInfo{IP: "127.0.0.1"}
	begun, err := server.Auth.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: token, Username: "admin", Password: "correct horse battery staple"}, client)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(begun.TOTP.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Auth.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupToken: token, SetupID: begun.SetupID, TOTPCode: code}, client); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func openApp(t *testing.T, dataDir string) *app.App {
	t.Helper()
	settings, err := config.File{DataDir: dataDir}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	server, err := app.New(t.Context(), app.Options{Settings: settings, Logger: slog.New(slog.DiscardHandler), Clock: clock.System{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() }) // closing twice is harmless; nothing to check
	return server
}

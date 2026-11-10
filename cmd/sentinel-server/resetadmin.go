// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/aazam476/sentinel/internal/server/app"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/localaccess"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// runResetAdmin handles "sentinel-server reset-admin" (SEC-08). args
// excludes the word "reset-admin".
func runResetAdmin(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("sentinel-server reset-admin", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", config.DefaultPath, "path to server.toml")
	dataDir := flags.String("data-dir", "", "data directory, overriding the config file")
	username := flags.String("username", "", "admin to reset (needed only when there are several)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsageError
	}

	newPassword, err := resetAdmin(*configPath, *dataDir, *username)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sentinel-server:", err)
		return exitFailure
	}
	_, _ = fmt.Fprintf(stdout, "The admin's password and two-factor authentication were reset.\n"+
		"New password: %s\nSign in with it and set up two-factor authentication again.\n", newPassword)
	return exitOK
}

func resetAdmin(configPath, dataDir, username string) (string, error) {
	settings, err := loadConfig(configPath, overrides{dataDir: dataDir})
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(settings.DataDir, "sentinel.db")); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no database in %s; check --data-dir", settings.DataDir)
	}
	if err := localaccess.Check(settings.DataDir); err != nil {
		return "", err
	}

	ctx := context.Background()
	server, err := app.New(ctx, app.Options{Settings: settings, Logger: slog.New(slog.DiscardHandler), Clock: clock.System{}})
	if err != nil {
		return "", err
	}
	newPassword, err := server.Auth.ResetAdmin(ctx, username)
	return newPassword, errors.Join(err, server.Close())
}

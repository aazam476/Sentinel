// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// runAudit handles "sentinel-server audit verify" and
// "sentinel-server audit export". args excludes the word "audit".
func runAudit(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "verify" && args[0] != "export") {
		_, _ = fmt.Fprintln(stderr, "Usage: sentinel-server audit verify|export [flags]")
		return exitUsageError
	}
	subcommand := args[0]

	flags := flag.NewFlagSet("sentinel-server audit "+subcommand, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", config.DefaultPath, "path to server.toml")
	dataDir := flags.String("data-dir", "", "data directory, overriding the config file")
	format := flags.String("format", "json", "export format: json or csv")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsageError
	}

	log, closeLog, err := openAuditLog(*configPath, *dataDir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sentinel-server:", err)
		return exitFailure
	}
	defer closeLog()

	if subcommand == "export" {
		return exportAudit(log, audit.Format(*format), stdout, stderr)
	}
	return verifyAudit(log, stdout, stderr)
}

// openAuditLog opens an existing database. Unlike the server, it refuses to
// create a new one: verifying an empty, freshly created log would wrongly
// report success.
func openAuditLog(configPath, dataDir string) (*audit.Log, func(), error) {
	settings, err := loadConfig(configPath, "", dataDir)
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(settings.DataDir, "sentinel.db")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("no database at %s; check --data-dir", path)
	}

	database, err := store.Open(context.Background(), path, clock.System{})
	if err != nil {
		return nil, nil, err
	}
	closeDatabase := func() { _ = database.Close() } // a read-only command has nothing left to save
	return audit.New(database), closeDatabase, nil
}

func verifyAudit(log *audit.Log, stdout, stderr io.Writer) int {
	result, err := log.Verify(context.Background())
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sentinel-server:", err)
		return exitFailure
	}
	if !result.Valid {
		_, _ = fmt.Fprintf(stdout, "audit log is NOT intact: %s (checked %d entries)\n", result.Problem, result.Checked)
		return exitFailure
	}
	_, _ = fmt.Fprintf(stdout, "audit log is intact: %d entries checked\n", result.Checked)
	return exitOK
}

func exportAudit(log *audit.Log, format audit.Format, stdout, stderr io.Writer) int {
	if err := log.Export(context.Background(), stdout, format); err != nil {
		_, _ = fmt.Fprintln(stderr, "sentinel-server:", err)
		return exitFailure
	}
	return exitOK
}

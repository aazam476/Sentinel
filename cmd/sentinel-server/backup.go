// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/aazam476/sentinel/internal/server/backup"
	"github.com/aazam476/sentinel/internal/server/config"
	"github.com/aazam476/sentinel/internal/server/localaccess"
	"github.com/aazam476/sentinel/internal/server/store"
)

// passphraseVariable can hold the backup passphrase, for scripted backups.
const passphraseVariable = "SENTINEL_BACKUP_PASSPHRASE"

// backupFlags are the flags backup and restore share.
type backupFlags struct {
	flags          *flag.FlagSet
	configPath     *string
	dataDir        *string
	passphraseFile *string
}

func newBackupFlags(name string, stderr io.Writer) backupFlags {
	flags := flag.NewFlagSet("sentinel-server "+name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	return backupFlags{
		flags:          flags,
		configPath:     flags.String("config", config.DefaultPath, "path to server.toml"),
		dataDir:        flags.String("data-dir", "", "data directory, overriding the config file"),
		passphraseFile: flags.String("passphrase-file", "", "read the passphrase from this file (default: $"+passphraseVariable+", else the first line of stdin)"),
	}
}

// parse parses args and returns the one file named, or an exit code.
func (b backupFlags) parse(args []string, stderr io.Writer) (string, int, bool) {
	if err := b.flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", exitOK, false
		}
		return "", exitUsageError, false
	}
	if b.flags.NArg() != 1 {
		_, _ = fmt.Fprintf(stderr, "Usage: %s [flags] <file>\n", b.flags.Name())
		return "", exitUsageError, false
	}
	return b.flags.Arg(0), exitOK, true
}

// passphrase reads the passphrase from the file, the environment or stdin.
func (b backupFlags) passphrase(stdin io.Reader) ([]byte, error) {
	var text string
	switch {
	case *b.passphraseFile != "":
		content, err := os.ReadFile(*b.passphraseFile)
		if err != nil {
			return nil, fmt.Errorf("read the passphrase: %w", err)
		}
		text = string(content)
	case os.Getenv(passphraseVariable) != "":
		text = os.Getenv(passphraseVariable)
	default:
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			return nil, errors.New("give the passphrase on stdin, in $" + passphraseVariable + " or with --passphrase-file")
		}
		text = line
	}
	text = strings.TrimRight(text, "\r\n")
	if len([]rune(text)) < backup.MinPassphraseLength {
		return nil, fmt.Errorf("the passphrase must be at least %d characters", backup.MinPassphraseLength)
	}
	return []byte(text), nil
}

// runBackup handles "sentinel-server backup <file>".
func runBackup(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options := newBackupFlags("backup", stderr)
	file, code, ok := options.parse(args, stderr)
	if !ok {
		return code
	}
	if err := writeBackup(options, file, stdin); err != nil {
		_, _ = fmt.Fprintln(stderr, "sentinel-server:", err)
		return exitFailure
	}
	_, _ = fmt.Fprintf(stdout, "Backed up to %s. Keep the passphrase: without it the backup can't be restored.\n", file)
	return exitOK
}

func writeBackup(options backupFlags, file string, stdin io.Reader) (err error) {
	settings, err := loadConfig(*options.configPath, "", *options.dataDir)
	if err != nil {
		return err
	}
	database := filepath.Join(settings.DataDir, backup.DatabaseName)
	if _, err := os.Stat(database); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("no database in %s; check --data-dir", settings.DataDir)
	}
	if err := localaccess.Check(settings.DataDir); err != nil {
		return err
	}
	passphrase, err := options.passphrase(stdin)
	if err != nil {
		return err
	}
	// A consistent copy, even while the server runs. Its name ends in .tmp,
	// so it isn't backed up a second time.
	snapshot := filepath.Join(settings.DataDir, "backup-snapshot.db.tmp")
	_ = os.Remove(snapshot) // left by a backup that was interrupted
	if err := store.Snapshot(context.Background(), database, snapshot); err != nil {
		return err
	}
	defer func() { _ = os.Remove(snapshot) }()
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: the file the admin named
	if err != nil {
		return fmt.Errorf("create the backup: %w", err)
	}
	defer func() {
		if closeErr := out.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("write the backup: %w", closeErr)
		}
		if err != nil {
			_ = os.Remove(file)
		}
	}()
	return backup.Write(out, passphrase, backup.Contents{DataDir: settings.DataDir, Database: snapshot, Config: *options.configPath})
}

// runRestore handles "sentinel-server restore <file>".
func runRestore(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options := newBackupFlags("restore", stderr)
	force := options.flags.Bool("force", false, "replace the database, keys and config file that are there (stop the server first)")
	file, code, ok := options.parse(args, stderr)
	if !ok {
		return code
	}
	keptConfig, err := restoreBackup(options, file, *force, stdin)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sentinel-server:", err)
		return exitFailure
	}
	_, _ = fmt.Fprintln(stdout, "Restored. Start the server to use it.")
	if keptConfig {
		_, _ = fmt.Fprintf(stdout, "The existing %s was kept; the backup's copy wasn't restored (use --force to replace it).\n", *options.configPath)
	}
	return exitOK
}

// restoreBackup decrypts the whole backup into a folder beside the data
// directory, and only once all of it has checked out moves it into place.
func restoreBackup(options backupFlags, file string, force bool, stdin io.Reader) (keptConfig bool, err error) {
	settings, err := loadConfig(*options.configPath, "", *options.dataDir)
	if err != nil {
		return false, err
	}
	dataDir := settings.DataDir
	if _, err := os.Stat(filepath.Join(dataDir, backup.DatabaseName)); err == nil && !force {
		return false, fmt.Errorf("%s already has a database; stop the server and pass --force to replace it", dataDir)
	}
	passphrase, err := options.passphrase(stdin)
	if err != nil {
		return false, err
	}
	in, err := os.Open(file) //nolint:gosec // G304: the file the admin named
	if err != nil {
		return false, fmt.Errorf("open the backup: %w", err)
	}
	defer func() { _ = in.Close() }() // read-only
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return false, fmt.Errorf("create the data directory: %w", err)
	}
	staging, err := os.MkdirTemp(dataDir, ".restore-")
	if err != nil {
		return false, fmt.Errorf("restore: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	var names []string
	err = backup.Read(in, passphrase, func(name string, data io.Reader) error {
		names = append(names, name)
		return writeStaged(filepath.Join(staging, filepath.FromSlash(name)), data)
	})
	if err != nil {
		return false, err
	}
	return moveIntoPlace(staging, names, dataDir, *options.configPath, force)
}

func writeStaged(file string, data io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a checked name inside the staging folder
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	_, err = io.Copy(out, data)
	return errors.Join(err, out.Close())
}

func moveIntoPlace(staging string, names []string, dataDir, configPath string, force bool) (bool, error) {
	for _, stale := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(filepath.Join(dataDir, backup.DatabaseName+stale)) // the old database's journals
	}
	keptConfig := false
	for _, name := range names {
		from := filepath.Join(staging, filepath.FromSlash(name))
		to := filepath.Join(dataDir, filepath.FromSlash(name))
		if name == backup.ConfigName {
			if _, err := os.Stat(configPath); err == nil && !force {
				keptConfig = true
				continue
			}
			to = configPath
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return false, fmt.Errorf("restore %s: %w", name, err)
		}
		if err := move(from, to); err != nil {
			return false, fmt.Errorf("restore %s: %w", name, err)
		}
	}
	return keptConfig, nil
}

// move renames from to to, or copies it when they are on different
// filesystems (the config file often is).
func move(from, to string) error {
	if os.Rename(from, to) == nil {
		return nil
	}
	in, err := os.Open(from) //nolint:gosec // G304: a file in the staging folder
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }() // read-only
	return writeReplacing(to, in)
}

func writeReplacing(file string, data io.Reader) error {
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: the config path the admin gave
	if err != nil {
		return err
	}
	_, err = io.Copy(out, data)
	return errors.Join(err, out.Close())
}

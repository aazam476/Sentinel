// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migration is one numbered .sql file.
type migration struct {
	version int
	name    string
	sql     string
}

// migrate applies every migration newer than the database's current version,
// each in its own transaction, and records it in schema_migrations.
func migrate(ctx context.Context, db *sql.DB, clock Clock) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return err
	}
	for _, next := range migrations {
		if next.version <= current {
			continue
		}
		if err := applyMigration(ctx, db, next, clock); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, next migration, clock Clock) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migration %s: begin: %w", next.name, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful commit

	if _, err := tx.ExecContext(ctx, next.sql); err != nil {
		return fmt.Errorf("migration %s: %w", next.name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		next.version, clock.Now().UnixMilli()); err != nil {
		return fmt.Errorf("migration %s: record version: %w", next.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration %s: commit: %w", next.name, err)
	}
	return nil
}

// loadMigrations reads every NNNN_name.sql file in files' migrations folder,
// sorted by number. Two files with the same number are an error.
func loadMigrations(files fs.FS) ([]migration, error) {
	names, err := fs.Glob(files, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}

	migrations := make([]migration, 0, len(names))
	seen := map[int]string{}
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		number, _, _ := strings.Cut(base, "_")
		version, err := strconv.Atoi(number)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("migration %s: name must start with a positive number like 0001_", base)
		}
		if other, duplicate := seen[version]; duplicate {
			return nil, fmt.Errorf("migrations %s and %s have the same number", other, base)
		}
		seen[version] = base

		contents, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", base, err)
		}
		migrations = append(migrations, migration{version: version, name: base, sql: string(contents)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

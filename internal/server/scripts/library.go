// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Errors from Service. They are never reassigned.
var (
	// ErrNotFound: no script, schedule, batch or run has that ID.
	ErrNotFound = errors.New("not found")
	// ErrExists: a script with that name already exists.
	ErrExists = errors.New("a script with that name already exists")
)

// InvalidError says which field of a script or schedule is wrong, and why.
type InvalidError struct {
	Field, Reason string
}

func (e *InvalidError) Error() string { return e.Field + ": " + e.Reason }

// Limits on scripts.
const (
	maxScriptBody   = 256 << 10
	maxNameLength   = 100
	maxDescription  = 1000
	defaultTimeout  = 600
	maxTimeoutHours = 24
)

// Languages lists the languages scripts can be written in.
func Languages() []string {
	return []string{protocol.LanguageBash, protocol.LanguageSh, protocol.LanguagePowerShell, protocol.LanguagePython}
}

// TargetSystems lists the systems a script can be limited to.
func TargetSystems() []string { return []string{"any", "linux", "darwin", "windows"} }

// Script is one script in the library.
type Script struct {
	ID             int64
	Name           string
	Description    string
	Language       string
	Body           string
	TargetOS       string
	TimeoutSeconds int
	CreatedAt      int64
	UpdatedAt      int64
}

// validate checks a script and fills in its defaults.
func (s *Script) validate() error {
	s.Name, s.Description = strings.TrimSpace(s.Name), strings.TrimSpace(s.Description)
	if s.TargetOS == "" {
		s.TargetOS = "any"
	}
	if s.TimeoutSeconds == 0 {
		s.TimeoutSeconds = defaultTimeout
	}
	switch {
	case s.Name == "" || utf8.RuneCountInString(s.Name) > maxNameLength:
		return &InvalidError{"name", fmt.Sprintf("give a name of up to %d characters", maxNameLength)}
	case utf8.RuneCountInString(s.Description) > maxDescription:
		return &InvalidError{"description", fmt.Sprintf("keep it to %d characters", maxDescription)}
	case !slices.Contains(Languages(), s.Language):
		return &InvalidError{"language", "choose bash, sh, powershell or python"}
	case !slices.Contains(TargetSystems(), s.TargetOS):
		return &InvalidError{"targetOS", "choose any, linux, darwin or windows"}
	case s.TimeoutSeconds < 1 || s.TimeoutSeconds > maxTimeoutHours*3600:
		return &InvalidError{"timeoutSeconds", "choose from 1 second to 24 hours"}
	case strings.TrimSpace(s.Body) == "" || len(s.Body) > maxScriptBody:
		return &InvalidError{"body", "write a script of up to 256 KB"}
	case !utf8.ValidString(s.Body):
		return &InvalidError{"body", "the script must be text"}
	}
	return nil
}

// Scripts lists the library, by name.
func (s *Service) Scripts(ctx context.Context) ([]Script, error) {
	var list []Script
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+scriptColumns+` FROM scripts ORDER BY name COLLATE NOCASE`)
		if err != nil {
			return fmt.Errorf("list scripts: %w", err)
		}
		defer func() { _ = rows.Close() }()
		list = []Script{}
		for rows.Next() {
			script, err := scanScript(rows)
			if err != nil {
				return err
			}
			list = append(list, script)
		}
		return rows.Err()
	})
	return list, err
}

const scriptColumns = `id, name, description, language, body, target_os, timeout_seconds, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanScript(row scanner) (Script, error) {
	var s Script
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Language, &s.Body, &s.TargetOS, &s.TimeoutSeconds, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Script{}, ErrNotFound
	}
	if err != nil {
		return Script{}, fmt.Errorf("read a script: %w", err)
	}
	return s, nil
}

// GetScript returns one script.
func (s *Service) GetScript(ctx context.Context, id int64) (Script, error) {
	var script Script
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		script, err = scanScript(tx.QueryRowContext(ctx, `SELECT `+scriptColumns+` FROM scripts WHERE id = ?`, id))
		return err
	})
	return script, err
}

// SaveScript adds a script (ID 0) or changes one, and audits it. Changing
// a script that a schedule runs on more than one machine needs a recent
// second-factor check (SEC-07): the schedule would run the new text there.
func (s *Service) SaveScript(ctx context.Context, actor auth.Session, request auth.RequestInfo, script Script) (Script, error) {
	if err := auth.Authorize(actor, auth.PermissionRunScripts); err != nil {
		return Script{}, err
	}
	if err := script.validate(); err != nil {
		return Script{}, err
	}
	if err := s.checkScriptChange(ctx, actor, script.ID); err != nil {
		return Script{}, err
	}
	now := s.clock.Now().UnixMilli()
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		var result sql.Result
		var err error
		if script.ID == 0 {
			result, err = tx.ExecContext(ctx, `INSERT INTO scripts (name, description, language, body, target_os, timeout_seconds, created_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, script.Name, script.Description, script.Language, script.Body, script.TargetOS,
				script.TimeoutSeconds, actor.UserID, now, now)
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE scripts SET name = ?, description = ?, language = ?, body = ?, target_os = ?,
				timeout_seconds = ?, updated_at = ? WHERE id = ?`, script.Name, script.Description, script.Language, script.Body,
				script.TargetOS, script.TimeoutSeconds, now, script.ID)
		}
		if err != nil {
			if isUniqueViolation(err) {
				return ErrExists
			}
			return fmt.Errorf("save the script: %w", err)
		}
		if script.ID == 0 {
			script.ID, err = result.LastInsertId()
			script.CreatedAt = now
		} else if changed, _ := result.RowsAffected(); changed == 0 {
			return ErrNotFound
		}
		script.UpdatedAt = now
		if err != nil {
			return fmt.Errorf("save the script: %w", err)
		}
		return s.audit.RecordInTx(ctx, tx, userEntry(actor, request, "script.save", map[string]any{"script": script.Name, "id": script.ID}))
	})
	return script, err
}

// DeleteScript removes a script and its schedules; its past runs stay.
func (s *Service) DeleteScript(ctx context.Context, actor auth.Session, request auth.RequestInfo, id int64) error {
	if err := auth.Authorize(actor, auth.PermissionRunScripts); err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		script, err := scanScript(tx.QueryRowContext(ctx, `SELECT `+scriptColumns+` FROM scripts WHERE id = ?`, id))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM scripts WHERE id = ?`, id); err != nil {
			return fmt.Errorf("delete the script: %w", err)
		}
		return s.audit.RecordInTx(ctx, tx, userEntry(actor, request, "script.delete", map[string]any{"script": script.Name, "id": id}))
	})
}

func userEntry(actor auth.Session, request auth.RequestInfo, action string, details map[string]any) audit.Entry {
	userID := actor.UserID
	return audit.Entry{UserID: &userID, Username: actor.Username, Action: action, Outcome: audit.OutcomeSuccess, IP: request.IP, Details: details}
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

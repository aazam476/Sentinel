// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// Enrollment token limits (§7.2): single use and 24 hours unless the admin
// asks for more, and never more than a week.
const (
	DefaultTokenLifetime = 24 * time.Hour
	MaxTokenLifetime     = 7 * 24 * time.Hour
	MaxTokenUses         = 1000
)

// EnrollmentToken is a token as admins see it. The token itself is never
// stored, so it can't be shown again after creation.
type EnrollmentToken struct {
	ID        int64
	Note      string
	CreatedBy string
	CreatedAt int64
	ExpiresAt int64
	MaxUses   int
	UseCount  int
	RevokedAt *int64
}

// UsableAt reports whether the token can still enroll an agent at now.
func (t EnrollmentToken) UsableAt(now int64) bool {
	return t.RevokedAt == nil && now < t.ExpiresAt && t.UseCount < t.MaxUses
}

// CreateTokenInput describes a new token. Zero values mean the defaults.
type CreateTokenInput struct {
	Note     string
	Lifetime time.Duration
	MaxUses  int
}

// CreatedToken is a new token and, this one time, its secret value.
type CreatedToken struct {
	EnrollmentToken
	Token string
}

// CreateEnrollmentToken makes a token for the install command.
func (s *Service) CreateEnrollmentToken(ctx context.Context, actor auth.Session, input CreateTokenInput, request auth.RequestInfo) (CreatedToken, error) {
	if err := auth.Authorize(actor, auth.PermissionManageEnrollment); err != nil {
		return CreatedToken{}, err
	}
	row, err := newTokenRow(input, s.now())
	if err != nil {
		return CreatedToken{}, err
	}
	token, tokenHash := auth.NewToken()

	var created CreatedToken
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		id, err := insertToken(ctx, tx, tokenHash, actor.UserID, row)
		if err != nil {
			return err
		}
		created = CreatedToken{EnrollmentToken: row, Token: token}
		created.ID, created.CreatedBy = id, actor.Username
		return s.audit.RecordInTx(ctx, tx, byUser(actor, request, "enrollment_token.create", map[string]any{
			"tokenId": id, "note": row.Note, "expiresAt": row.ExpiresAt, "maxUses": row.MaxUses,
		}))
	})
	return created, err
}

func newTokenRow(input CreateTokenInput, now int64) (EnrollmentToken, error) {
	note, err := cleanText("note", input.Note, maxNoteLength)
	if err != nil {
		return EnrollmentToken{}, err
	}
	lifetime := input.Lifetime
	if lifetime == 0 {
		lifetime = DefaultTokenLifetime
	}
	if lifetime < time.Hour || lifetime > MaxTokenLifetime {
		return EnrollmentToken{}, &auth.ValidationError{Field: "expiresInHours", Message: "Tokens last between 1 hour and 7 days."}
	}
	maxUses := input.MaxUses
	if maxUses == 0 {
		maxUses = 1
	}
	if maxUses < 1 || maxUses > MaxTokenUses {
		return EnrollmentToken{}, &auth.ValidationError{Field: "maxUses", Message: "A token can be used between 1 and 1,000 times."}
	}
	return EnrollmentToken{Note: note, CreatedAt: now, ExpiresAt: now + lifetime.Milliseconds(), MaxUses: maxUses}, nil
}

// ListEnrollmentTokens returns tokens that are still usable or ended in the
// last week, newest first.
func (s *Service) ListEnrollmentTokens(ctx context.Context, actor auth.Session) ([]EnrollmentToken, error) {
	if err := auth.Authorize(actor, auth.PermissionManageEnrollment); err != nil {
		return nil, err
	}
	var tokens []EnrollmentToken
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		tokens, err = listTokens(ctx, tx, s.now()-MaxTokenLifetime.Milliseconds())
		return err
	})
	return tokens, err
}

// RevokeEnrollmentToken stops a token from enrolling any more agents.
// Agents it already enrolled are unaffected.
func (s *Service) RevokeEnrollmentToken(ctx context.Context, actor auth.Session, tokenID int64, request auth.RequestInfo) error {
	if err := auth.Authorize(actor, auth.PermissionManageEnrollment); err != nil {
		return err
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		token, err := findToken(ctx, tx, "id = ?", tokenID)
		if errors.Is(err, errTokenNotFound) {
			return auth.ErrNotFound
		}
		if err != nil || token.RevokedAt != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE enrollment_tokens SET revoked_at = ? WHERE id = ?`, s.now(), tokenID); err != nil {
			return fmt.Errorf("revoke enrollment token: %w", err)
		}
		return s.audit.RecordInTx(ctx, tx, byUser(actor, request, "enrollment_token.revoke", map[string]any{
			"tokenId": tokenID, "note": token.Note,
		}))
	})
}

// errTokenNotFound means no token matched. It is never reassigned.
var errTokenNotFound = errors.New("enrollment token not found")

//nolint:gosec // G101: a SQL query that reads token metadata, not a credential
const selectToken = `SELECT t.id, t.note, u.username, t.created_at, t.expires_at, t.max_uses, t.use_count, t.revoked_at
	FROM enrollment_tokens t JOIN users u ON u.id = t.created_by`

func insertToken(ctx context.Context, tx *sql.Tx, tokenHash string, createdBy int64, row EnrollmentToken) (int64, error) {
	result, err := tx.ExecContext(ctx,
		`INSERT INTO enrollment_tokens (token_hash, note, created_by, created_at, expires_at, max_uses) VALUES (?, ?, ?, ?, ?, ?)`,
		tokenHash, row.Note, createdBy, row.CreatedAt, row.ExpiresAt, row.MaxUses)
	if err != nil {
		return 0, fmt.Errorf("insert enrollment token: %w", err)
	}
	return result.LastInsertId()
}

// findToken returns the token matching condition, a SQL condition with
// one parameter, always written in this package.
func findToken(ctx context.Context, tx *sql.Tx, condition string, value any) (EnrollmentToken, error) {
	row := tx.QueryRowContext(ctx, selectToken+" WHERE t."+condition, value)
	token, err := scanToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return EnrollmentToken{}, errTokenNotFound
	}
	return token, err
}

func listTokens(ctx context.Context, tx *sql.Tx, endedAfter int64) ([]EnrollmentToken, error) {
	rows, err := tx.QueryContext(ctx, selectToken+
		` WHERE t.expires_at > ? AND (t.revoked_at IS NULL OR t.revoked_at > ?) ORDER BY t.created_at DESC, t.id DESC`,
		endedAfter, endedAfter)
	if err != nil {
		return nil, fmt.Errorf("list enrollment tokens: %w", err)
	}
	defer func() { _ = rows.Close() }() // read-only; nothing to report

	tokens := []EnrollmentToken{}
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func scanToken(row interface{ Scan(...any) error }) (EnrollmentToken, error) {
	var token EnrollmentToken
	err := row.Scan(&token.ID, &token.Note, &token.CreatedBy, &token.CreatedAt, &token.ExpiresAt,
		&token.MaxUses, &token.UseCount, &token.RevokedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return EnrollmentToken{}, fmt.Errorf("read enrollment token: %w", err)
	}
	return token, err
}

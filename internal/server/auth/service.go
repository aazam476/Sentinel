// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"path/filepath"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// Database is what the auth package needs from the store.
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// Encrypter encrypts stored secrets (internal/server/secrets).
type Encrypter interface {
	Encrypt(plaintext, associatedData []byte) ([]byte, error)
	Decrypt(sealed, associatedData []byte) ([]byte, error)
}

// AuditRecorder records audit entries (internal/server/audit).
type AuditRecorder interface {
	Record(ctx context.Context, entry audit.Entry) error
	RecordInTx(ctx context.Context, tx *sql.Tx, entry audit.Entry) error
}

// Options holds what NewService needs.
type Options struct {
	Database Database
	Secrets  Encrypter
	AuditLog AuditRecorder
	Clock    Clock
	// DataDir is the data directory, where servers before v0.1.0-beta.1
	// left a setup token file that is now removed (ADR 0037).
	DataDir string
}

// RequestInfo describes the client making a request, for the audit log and
// rate limiting.
type RequestInfo struct {
	IP        string
	UserAgent string
}

// Service implements sign-in, sessions and user management.
type Service struct {
	database       Database
	secrets        Encrypter
	audit          AuditRecorder
	clock          Clock
	hasher         *passwordHasher
	policy         *passwordPolicy
	ipLimiter      *limiter
	usernameLimit  *limiter
	setupTokenPath string
	pendingSetups  *pendingSetups
	ceremonies     *ceremonies
}

// NewService returns a Service. Creating it hashes a dummy password and
// loads the common-password list, which takes a moment, so create one at
// startup and share it.
func NewService(options Options) *Service {
	return &Service{
		database:       options.Database,
		secrets:        options.Secrets,
		audit:          options.AuditLog,
		clock:          options.Clock,
		hasher:         newPasswordHasher(),
		policy:         newPasswordPolicy(),
		ipLimiter:      newLimiter(options.Clock, false),
		usernameLimit:  newLimiter(options.Clock, true),
		setupTokenPath: filepath.Join(options.DataDir, setupTokenFileName),
		pendingSetups:  newPendingSetups(options.Clock),
		ceremonies:     newCeremonies(options.Clock),
	}
}

// now returns the current time as UTC Unix milliseconds.
func (s *Service) now() int64 {
	return s.clock.Now().UnixMilli()
}

// auditEntry fills in the request details every audit entry needs.
func auditEntry(request RequestInfo, action string, outcome audit.Outcome, details map[string]any) audit.Entry {
	return audit.Entry{Action: action, Outcome: outcome, IP: request.IP, Details: details}
}

// forUser adds the user to an audit entry.
func forUser(entry audit.Entry, userID int64, username string) audit.Entry {
	entry.UserID = &userID
	entry.Username = username
	return entry
}

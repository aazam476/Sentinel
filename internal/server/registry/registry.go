// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/distribution/reference"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// Errors from Service. They are never reassigned.
var (
	// ErrNotFound: no login has that ID.
	ErrNotFound = errors.New("there is no registry login with that ID")
	// ErrExists: the registry already has a login; remove it first.
	ErrExists = errors.New("that registry already has a login")
)

// host matches a registry's host name, with an optional port.
var host = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?(:[0-9]{1,5})?$`)

// Database runs transactions (internal/server/store).
type Database interface {
	Read(ctx context.Context, work func(tx *sql.Tx) error) error
	Write(ctx context.Context, work func(tx *sql.Tx) error) error
}

// AuditLog records changes in the same transaction as the change.
type AuditLog interface {
	RecordInTx(ctx context.Context, tx *sql.Tx, entry audit.Entry) error
}

// Sealer encrypts and decrypts secrets (internal/server/secrets).
type Sealer interface {
	Encrypt(plaintext, associatedData []byte) ([]byte, error)
	Decrypt(sealed, associatedData []byte) ([]byte, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Service manages registry logins and image update results.
type Service struct {
	database Database
	audit    AuditLog
	sealer   Sealer
	clock    Clock
}

// NewService returns a Service.
func NewService(database Database, auditLog AuditLog, sealer Sealer, clock Clock) *Service {
	return &Service{database: database, audit: auditLog, sealer: sealer, clock: clock}
}

// Login is a registry login as the API shows it: never with its password.
type Login struct {
	ID            int64
	ServerAddress string
	Username      string
	CreatedAt     int64
}

// LoginInput is a new login.
type LoginInput struct {
	ServerAddress, Username, Password string
}

// HostOf returns the registry host an image reference pulls from: docker.io
// for Docker Hub's short names, otherwise the reference's first part.
func HostOf(imageRef string) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", &auth.ValidationError{Field: "reference", Message: fmt.Sprintf("%q isn’t an image name.", imageRef)}
	}
	return reference.Domain(named), nil
}

func validate(input LoginInput) error {
	address := strings.ToLower(strings.TrimSpace(input.ServerAddress))
	switch {
	case !host.MatchString(address):
		return &auth.ValidationError{Field: "serverAddress", Message: "Enter the registry’s host name, such as ghcr.io."}
	case input.Username == "" || len(input.Username) > 255:
		return &auth.ValidationError{Field: "username", Message: "Enter the user name, up to 255 characters."}
	case input.Password == "" || len(input.Password) > 4096:
		return &auth.ValidationError{Field: "password", Message: "Enter the password or token, up to 4,096 characters."}
	}
	return nil
}

// Add stores a login, its password encrypted (SEC-42), and audits it.
func (s *Service) Add(ctx context.Context, actor auth.Session, request auth.RequestInfo, input LoginInput) (Login, error) {
	if err := validate(input); err != nil {
		return Login{}, err
	}
	login := Login{ServerAddress: strings.ToLower(strings.TrimSpace(input.ServerAddress)), Username: input.Username,
		CreatedAt: s.clock.Now().UnixMilli()}
	sealed, err := s.sealer.Encrypt([]byte(input.Password), []byte(login.ServerAddress))
	if err != nil {
		return Login{}, fmt.Errorf("encrypt the registry password: %w", err)
	}
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `INSERT INTO registry_credentials
			(server_address, username, password, created_by, created_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (server_address) DO NOTHING RETURNING id`,
			login.ServerAddress, login.Username, sealed, actor.UserID, login.CreatedAt).Scan(&login.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrExists
		}
		if err != nil {
			return fmt.Errorf("store the registry login: %w", err)
		}
		return s.audit.RecordInTx(ctx, tx, entry(actor, request, "registry.add",
			map[string]any{"serverAddress": login.ServerAddress, "username": login.Username}))
	})
	if err != nil {
		return Login{}, err
	}
	return login, nil
}

// List returns every login, without passwords.
func (s *Service) List(ctx context.Context) ([]Login, error) {
	logins := []Login{}
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, server_address, username, created_at
			FROM registry_credentials ORDER BY server_address`)
		if err != nil {
			return fmt.Errorf("list registry logins: %w", err)
		}
		defer rows.Close() //nolint:errcheck // read-only
		for rows.Next() {
			var login Login
			if err := rows.Scan(&login.ID, &login.ServerAddress, &login.Username, &login.CreatedAt); err != nil {
				return fmt.Errorf("read a registry login: %w", err)
			}
			logins = append(logins, login)
		}
		return rows.Err()
	})
	return logins, err
}

// Remove deletes a login and audits it.
func (s *Service) Remove(ctx context.Context, actor auth.Session, request auth.RequestInfo, id int64) error {
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		var address string
		err := tx.QueryRowContext(ctx, `DELETE FROM registry_credentials WHERE id = ? RETURNING server_address`, id).Scan(&address)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("remove the registry login: %w", err)
		}
		return s.audit.RecordInTx(ctx, tx, entry(actor, request, "registry.remove", map[string]any{"serverAddress": address}))
	})
}

// AuthFor returns the login for the registry an image comes from, decrypted
// for one request, or nil when there is none.
func (s *Service) AuthFor(ctx context.Context, imageRef string) (*protocol.RegistryAuth, error) {
	registryHost, err := HostOf(imageRef)
	if err != nil {
		return nil, err
	}
	var username string
	var sealed []byte
	err = s.database.Read(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT username, password FROM registry_credentials WHERE server_address = ?`,
			registryHost).Scan(&username, &sealed)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // no login is the normal case for public images
	}
	if err != nil {
		return nil, fmt.Errorf("read the registry login: %w", err)
	}
	password, err := s.sealer.Decrypt(sealed, []byte(registryHost))
	if err != nil {
		return nil, fmt.Errorf("decrypt the registry login for %s: %w", registryHost, err)
	}
	return &protocol.RegistryAuth{ServerAddress: registryHost, Username: username, Password: string(password)}, nil
}

func entry(actor auth.Session, request auth.RequestInfo, action string, details map[string]any) audit.Entry {
	userID := actor.UserID
	return audit.Entry{UserID: &userID, Username: actor.Username, Action: action, Outcome: audit.OutcomeSuccess,
		IP: request.IP, Details: details}
}

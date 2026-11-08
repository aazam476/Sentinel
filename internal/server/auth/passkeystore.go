// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Passkey is a stored passkey, as its owner sees it.
type Passkey struct {
	ID         int64
	Name       string
	CreatedAt  int64
	LastUsedAt *int64
}

// webAuthnUser adapts a user to the WebAuthn library's User interface.
type webAuthnUser struct {
	handle      []byte
	username    string
	credentials []webauthn.Credential
}

func (u webAuthnUser) WebAuthnID() []byte                         { return u.handle }
func (u webAuthnUser) WebAuthnName() string                       { return u.username }
func (u webAuthnUser) WebAuthnDisplayName() string                { return u.username }
func (u webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// webAuthnHandleBytes is the user handle's length; 64 bytes is the most
// WebAuthn allows and what it recommends.
const webAuthnHandleBytes = 64

// loadWebAuthnUser returns userID's handle and passkeys, creating the
// handle on first use (which needs a write transaction).
func loadWebAuthnUser(ctx context.Context, tx *sql.Tx, userID int64, username string) (webAuthnUser, error) {
	user := webAuthnUser{username: username}
	if err := tx.QueryRowContext(ctx, `SELECT webauthn_handle FROM users WHERE id = ?`, userID).Scan(&user.handle); err != nil {
		return webAuthnUser{}, fmt.Errorf("read passkey user handle: %w", err)
	}
	if len(user.handle) == 0 {
		user.handle = make([]byte, webAuthnHandleBytes)
		// crypto/rand.Read never returns an error: if the system's random
		// source fails, Go stops the program rather than hand out
		// predictable bytes.
		_, _ = rand.Read(user.handle)
		if _, err := tx.ExecContext(ctx, `UPDATE users SET webauthn_handle = ? WHERE id = ?`, user.handle, userID); err != nil {
			return webAuthnUser{}, fmt.Errorf("save passkey user handle: %w", err)
		}
	}

	credentials, err := loadCredentials(ctx, tx, userID)
	if err != nil {
		return webAuthnUser{}, err
	}
	user.credentials = credentials
	return user, nil
}

func loadCredentials(ctx context.Context, tx *sql.Tx, userID int64) ([]webauthn.Credential, error) {
	rows, err := tx.QueryContext(ctx, `SELECT credential_json FROM passkeys WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("read passkeys: %w", err)
	}
	defer func() { _ = rows.Close() }() // a close error after reading is not actionable

	var credentials []webauthn.Credential
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return nil, fmt.Errorf("read passkey: %w", err)
		}
		var credential webauthn.Credential
		if err := json.Unmarshal([]byte(encoded), &credential); err != nil {
			return nil, fmt.Errorf("decode passkey: %w", err)
		}
		credentials = append(credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read passkeys: %w", err)
	}
	return credentials, nil
}

// newPasskeyRow is what insertPasskey stores.
type newPasskeyRow struct {
	userID     int64
	name       string
	credential *webauthn.Credential
	createdAt  int64
}

func insertPasskey(ctx context.Context, tx *sql.Tx, row newPasskeyRow) error {
	encoded, err := json.Marshal(row.credential)
	if err != nil {
		return fmt.Errorf("encode passkey: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO passkeys
		(user_id, credential_id, public_key, sign_count, name, credential_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.userID, row.credential.ID, row.credential.PublicKey, row.credential.Authenticator.SignCount, row.name, string(encoded), row.createdAt)
	if err != nil {
		return fmt.Errorf("save passkey: %w", err)
	}
	return nil
}

// recordPasskeyUse stores the credential's new signature counter and flags
// after a sign-in.
func recordPasskeyUse(ctx context.Context, tx *sql.Tx, credential *webauthn.Credential, now int64) error {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encode passkey: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE passkeys SET sign_count = ?, credential_json = ?, last_used_at = ? WHERE credential_id = ?`,
		credential.Authenticator.SignCount, string(encoded), now, credential.ID)
	if err != nil {
		return fmt.Errorf("record passkey use: %w", err)
	}
	return nil
}

func listPasskeys(ctx context.Context, tx *sql.Tx, userID int64) ([]Passkey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, name, created_at, last_used_at FROM passkeys WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	defer func() { _ = rows.Close() }() // a close error after reading is not actionable

	passkeys := []Passkey{}
	for rows.Next() {
		var passkey Passkey
		if err := rows.Scan(&passkey.ID, &passkey.Name, &passkey.CreatedAt, &passkey.LastUsedAt); err != nil {
			return nil, fmt.Errorf("list passkeys: %w", err)
		}
		passkeys = append(passkeys, passkey)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	return passkeys, nil
}

// deletePasskey removes one of userID's passkeys and reports whether it
// existed.
func deletePasskey(ctx context.Context, tx *sql.Tx, userID, passkeyID int64) (bool, error) {
	result, err := tx.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ? AND user_id = ?`, passkeyID, userID)
	if err != nil {
		return false, fmt.Errorf("delete passkey: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete passkey: %w", err)
	}
	return deleted > 0, nil
}

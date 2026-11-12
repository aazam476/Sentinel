// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// User is a user account.
type User struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       Role   `json:"role"`
	CreatedAt  int64  `json:"createdAt"`
	DisabledAt *int64 `json:"disabledAt"`
	// HasSecondFactor is whether they have TOTP or a passkey set up.
	HasSecondFactor bool `json:"hasSecondFactor"`
	passwordHash    string
	lockedUntil     *int64
}

// errUserNotFound means no user matched. It is never reassigned.
var errUserNotFound = errors.New("user not found")

const selectUser = `SELECT id, username, password_hash, role, created_at, disabled_at, locked_until,
	EXISTS (SELECT 1 FROM totp_secrets t WHERE t.user_id = users.id AND t.confirmed_at IS NOT NULL)
	OR EXISTS (SELECT 1 FROM passkeys p WHERE p.user_id = users.id)
	FROM users`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var user User
	var role string
	err := row.Scan(&user.ID, &user.Username, &user.passwordHash, &role, &user.CreatedAt, &user.DisabledAt, &user.lockedUntil, &user.HasSecondFactor)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, errUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("read user: %w", err)
	}
	user.Role = Role(role)
	return user, nil
}

func findUserByUsername(ctx context.Context, tx *sql.Tx, username string) (User, error) {
	return scanUser(tx.QueryRowContext(ctx, selectUser+` WHERE username = ?`, username))
}

func findUserByID(ctx context.Context, tx *sql.Tx, id int64) (User, error) {
	return scanUser(tx.QueryRowContext(ctx, selectUser+` WHERE id = ?`, id))
}

func listUsers(ctx context.Context, tx *sql.Tx) ([]User, error) {
	rows, err := tx.QueryContext(ctx, selectUser+` ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }() // a close error after reading is not actionable

	users := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

func countUsers(ctx context.Context, tx *sql.Tx) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

// countActiveAdmins counts admins who aren't disabled.
func countActiveAdmins(ctx context.Context, tx *sql.Tx) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}

// newUserRow is what insertUser stores.
type newUserRow struct {
	username     string
	passwordHash string
	role         Role
	createdAt    int64
}

func insertUser(ctx context.Context, tx *sql.Tx, row newUserRow) (int64, error) {
	result, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)`,
		row.username, row.passwordHash, string(row.role), row.createdAt)
	if err != nil {
		return 0, fmt.Errorf("create user %s: %w", row.username, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("create user %s: %w", row.username, err)
	}
	return id, nil
}

func updateUserRole(ctx context.Context, tx *sql.Tx, userID int64, role Role) error {
	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, string(role), userID); err != nil {
		return fmt.Errorf("change role of user %d: %w", userID, err)
	}
	return nil
}

// setUserDisabled disables the user at disabledAt, or enables them when
// disabledAt is nil.
func setUserDisabled(ctx context.Context, tx *sql.Tx, userID int64, disabledAt *int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, disabledAt, userID); err != nil {
		return fmt.Errorf("change disabled state of user %d: %w", userID, err)
	}
	return nil
}

// setUserLockedUntil records an account lock (SEC-05), or clears it when
// lockedUntil is nil.
func setUserLockedUntil(ctx context.Context, tx *sql.Tx, userID int64, lockedUntil *int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE users SET locked_until = ? WHERE id = ?`, lockedUntil, userID); err != nil {
		return fmt.Errorf("lock user %d: %w", userID, err)
	}
	return nil
}

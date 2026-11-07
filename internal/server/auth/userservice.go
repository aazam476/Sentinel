// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aazam476/sentinel/internal/server/audit"
)

// CreateUserInput is a new user's details. The user sets up 2FA at their
// first sign-in.
type CreateUserInput struct {
	Username string
	Password string
	Role     Role
}

// UpdateUserInput changes a user. Nil fields are left as they are.
type UpdateUserInput struct {
	Role     *Role
	Disabled *bool
}

// authorize returns ErrForbidden unless session's role has permission.
func authorize(session Session, permission Permission) error {
	if !session.IsSignedIn() {
		return ErrNotAuthenticated
	}
	if !session.Role.Can(permission) {
		return ErrForbidden
	}
	return nil
}

// ListUsers returns every user, for admins.
func (s *Service) ListUsers(ctx context.Context, actor Session) ([]User, error) {
	if err := authorize(actor, PermissionManageUsers); err != nil {
		return nil, err
	}
	var users []User
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		users, err = listUsers(ctx, tx)
		return err
	})
	return users, err
}

// CreateUser adds a user. Admins only, with a recent second-factor check
// (SEC-07).
func (s *Service) CreateUser(ctx context.Context, actor Session, input CreateUserInput, request RequestInfo) (User, error) {
	if err := s.authorizeUserChange(actor); err != nil {
		return User{}, err
	}
	username, err := validateUsername(input.Username)
	if err != nil {
		return User{}, err
	}
	if err := validateRole(input.Role); err != nil {
		return User{}, err
	}
	if err := s.validatePassword(username, input.Password); err != nil {
		return User{}, err
	}
	passwordHash, err := s.hasher.Hash(ctx, input.Password)
	if err != nil {
		return User{}, err
	}

	var created User
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		created, err = s.insertNewUser(ctx, tx, newUserRow{username: username, passwordHash: passwordHash, role: input.Role, createdAt: s.now()})
		if err != nil {
			return err
		}
		entry := forUser(auditEntry(request, "user.create", audit.OutcomeSuccess,
			map[string]any{"userId": created.ID, "username": created.Username, "role": created.Role}), actor.UserID, actor.Username)
		return s.audit.RecordInTx(ctx, tx, entry)
	})
	return created, err
}

func (s *Service) insertNewUser(ctx context.Context, tx *sql.Tx, row newUserRow) (User, error) {
	if _, err := findUserByUsername(ctx, tx, row.username); err == nil {
		return User{}, ErrUsernameTaken
	} else if !errors.Is(err, errUserNotFound) {
		return User{}, err
	}
	id, err := insertUser(ctx, tx, row)
	if err != nil {
		return User{}, err
	}
	return findUserByID(ctx, tx, id)
}

// UpdateUser changes a user's role or disabled state. Admins only, with a
// recent second-factor check (SEC-07). Disabling a user ends their sessions.
// A change that would leave no active admin is refused.
func (s *Service) UpdateUser(ctx context.Context, actor Session, userID int64, input UpdateUserInput, request RequestInfo) (User, error) {
	if err := s.authorizeUserChange(actor); err != nil {
		return User{}, err
	}
	if input.Role != nil {
		if err := validateRole(*input.Role); err != nil {
			return User{}, err
		}
	}

	var updated User
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		target, err := findUserByID(ctx, tx, userID)
		if errors.Is(err, errUserNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := s.applyUserChanges(ctx, tx, actor, target, input, request); err != nil {
			return err
		}
		updated, err = findUserByID(ctx, tx, userID)
		return err
	})
	return updated, err
}

func (s *Service) applyUserChanges(ctx context.Context, tx *sql.Tx, actor Session, target User, input UpdateUserInput, request RequestInfo) error {
	if input.Role != nil && *input.Role != target.Role {
		if err := updateUserRole(ctx, tx, target.ID, *input.Role); err != nil {
			return err
		}
		if err := s.recordUserChange(ctx, tx, actor, request, "user.role.change",
			map[string]any{"userId": target.ID, "username": target.Username, "from": target.Role, "to": *input.Role}); err != nil {
			return err
		}
	}
	if input.Disabled != nil && *input.Disabled != (target.DisabledAt != nil) {
		if err := s.setDisabled(ctx, tx, actor, target, *input.Disabled, request); err != nil {
			return err
		}
	}
	return ensureAnActiveAdmin(ctx, tx)
}

func (s *Service) setDisabled(ctx context.Context, tx *sql.Tx, actor Session, target User, disable bool, request RequestInfo) error {
	action := "user.enable"
	var disabledAt *int64
	if disable {
		action = "user.disable"
		now := s.now()
		disabledAt = &now
		if err := deleteSessionsForUser(ctx, tx, target.ID); err != nil {
			return err
		}
	}
	if err := setUserDisabled(ctx, tx, target.ID, disabledAt); err != nil {
		return err
	}
	return s.recordUserChange(ctx, tx, actor, request, action, map[string]any{"userId": target.ID, "username": target.Username})
}

func (s *Service) recordUserChange(ctx context.Context, tx *sql.Tx, actor Session, request RequestInfo, action string, details map[string]any) error {
	entry := forUser(auditEntry(request, action, audit.OutcomeSuccess, details), actor.UserID, actor.Username)
	return s.audit.RecordInTx(ctx, tx, entry)
}

// ensureAnActiveAdmin fails the transaction if no enabled admin would remain.
func ensureAnActiveAdmin(ctx context.Context, tx *sql.Tx) error {
	count, err := countActiveAdmins(ctx, tx)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrLastAdmin
	}
	return nil
}

// authorizeUserChange checks the admin permission and the step-up window.
func (s *Service) authorizeUserChange(actor Session) error {
	if err := authorize(actor, PermissionManageUsers); err != nil {
		return err
	}
	return s.RequireRecentSecondFactor(actor)
}

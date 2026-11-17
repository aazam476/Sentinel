// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

const newPassword = "a-new-and-long-passphrase-42" //nolint:gosec // G101: a password for tests

func TestChangePassword(t *testing.T) {
	t.Parallel()

	t.Run("changes it with the current one, ends the user's other sessions and audits it", func(t *testing.T) {
		h := newHarness(t)
		admin, token, _ := h.setUpAdmin(t)
		other := h.signIn(t, admin)
		session := h.authenticate(t, token)
		if err := h.service.ChangePassword(t.Context(), session, admin.password, newPassword, testClient()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.Authenticate(t.Context(), other); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("another session survived: %v", err)
		}
		h.authenticate(t, token) // this one stays
		if _, err := h.service.Login(t.Context(), auth.LoginInput{Username: "admin", Password: admin.password}, testClient()); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("the old password still works: %v", err)
		}
		if _, err := h.service.Login(t.Context(), auth.LoginInput{Username: "admin", Password: newPassword}, testClient()); err != nil {
			t.Errorf("the new password doesn't: %v", err)
		}
		if !contains(h.auditActions(t), "auth.password.change:success") {
			t.Errorf("audit = %v", h.auditActions(t))
		}
	})

	t.Run("refuses a wrong current password, counting it as a failed attempt", func(t *testing.T) {
		h := newHarness(t)
		_, token, _ := h.setUpAdmin(t)
		session := h.authenticate(t, token)
		if err := h.service.ChangePassword(t.Context(), session, "not-the-password", newPassword, testClient()); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("err = %v", err)
		}
		if !contains(h.auditActions(t), "auth.password.change:failure") {
			t.Errorf("audit = %v", h.auditActions(t))
		}
	})

	t.Run("refuses a session that hasn't finished signing in, and a client in backoff", func(t *testing.T) {
		h := newHarness(t)
		admin, _, _ := h.setUpAdmin(t)
		result, err := h.service.Login(t.Context(), auth.LoginInput{Username: "admin", Password: admin.password}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		pending := h.authenticate(t, result.SessionToken)
		if err := h.service.ChangePassword(t.Context(), pending, admin.password, newPassword, testClient()); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("half signed in: %v", err)
		}
		session := h.authenticate(t, h.signIn(t, admin))
		for _, wrong := range []string{"wrong password one", "wrong password two", "wrong password three"} {
			_ = h.service.ChangePassword(t.Context(), session, wrong, newPassword, testClient())
		}
		var tooMany *auth.TooManyAttemptsError
		if err := h.service.ChangePassword(t.Context(), session, admin.password, newPassword, testClient()); !errors.As(err, &tooMany) {
			t.Errorf("in backoff: %v", err)
		}
	})

	t.Run("refuses a new password the policy refuses", func(t *testing.T) {
		h := newHarness(t)
		admin, token, _ := h.setUpAdmin(t)
		err := h.service.ChangePassword(t.Context(), h.authenticate(t, token), admin.password, "short", testClient())
		var invalid *auth.ValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("err = %v, want an invalid input", err)
		}
	})
}

func TestSEC07_ResetPassword(t *testing.T) {
	t.Parallel()

	t.Run("an admin resets another user's password, ending their sessions and unlocking them", func(t *testing.T) {
		h := newHarness(t)
		_, adminToken, _ := h.setUpAdmin(t)
		carol := h.createUser(t, adminToken, "carol", auth.RoleOperator)
		carolToken := h.signIn(t, carol)
		admin := h.authenticate(t, adminToken)
		user := userNamed(t, h, admin, "carol")
		if err := h.service.ResetPassword(t.Context(), admin, user.ID, newPassword, testClient()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.Authenticate(t.Context(), carolToken); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("carol is still signed in: %v", err)
		}
		if _, err := h.service.Login(t.Context(), auth.LoginInput{Username: "carol", Password: newPassword}, testClient()); err != nil {
			t.Errorf("the new password doesn't work: %v", err)
		}
		if !contains(h.auditActions(t), "user.password.reset:success") {
			t.Errorf("audit = %v", h.auditActions(t))
		}
	})

	t.Run("needs an admin with a recent second factor", func(t *testing.T) {
		h := newHarness(t)
		_, adminToken, _ := h.setUpAdmin(t)
		carol := h.createUser(t, adminToken, "carol", auth.RoleOperator)
		admin := h.authenticate(t, adminToken)
		user := userNamed(t, h, admin, "carol")
		operator := h.authenticate(t, h.signIn(t, carol))
		if err := h.service.ResetPassword(t.Context(), operator, user.ID, newPassword, testClient()); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("an operator: %v", err)
		}
		h.clock.Advance(16 * time.Minute)
		admin = h.authenticate(t, adminToken)
		if err := h.service.ResetPassword(t.Context(), admin, user.ID, newPassword, testClient()); !errors.Is(err, auth.ErrStepUpRequired) {
			t.Errorf("a stale admin: %v", err)
		}
	})

	t.Run("refuses a new password the policy refuses", func(t *testing.T) {
		h := newHarness(t)
		_, adminToken, _ := h.setUpAdmin(t)
		admin := h.authenticate(t, adminToken)
		var invalid *auth.ValidationError
		if err := h.service.ResetPassword(t.Context(), admin, admin.UserID, "short", testClient()); !errors.As(err, &invalid) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("says when there is no such user", func(t *testing.T) {
		h := newHarness(t)
		_, adminToken, _ := h.setUpAdmin(t)
		if err := h.service.ResetPassword(t.Context(), h.authenticate(t, adminToken), 9999, newPassword, testClient()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})
}

func userNamed(t *testing.T, h *harness, admin auth.Session, username string) auth.User {
	t.Helper()
	users, err := h.service.ListUsers(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if user.Username == username {
			return user
		}
	}
	t.Fatalf("no user %s", username)
	return auth.User{}
}

func TestPasswordChangesGiveUpWhenHashingIsBusy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	admin, adminToken, _ := h.setUpAdmin(t)
	session := h.authenticate(t, adminToken)
	release := h.service.FillHasher()
	defer release()
	// The database steps finish well within the time; the hasher then waits
	// for a slot until it runs out.
	within := func() context.Context {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		t.Cleanup(cancel)
		return ctx
	}
	if err := h.service.ChangePassword(within(), session, admin.password, newPassword, testClient()); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("ChangePassword = %v, want it to give up", err)
	}
	if err := h.service.ResetPassword(within(), session, session.UserID, newPassword, testClient()); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("ResetPassword = %v, want it to give up", err)
	}
}

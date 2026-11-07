// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

func TestSEC07_ChangingUsersNeedsARecentSecondFactor(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	admin, token, _ := h.setUpAdmin(t)
	newUser := auth.CreateUserInput{Username: "carol", Password: strongPassword, Role: auth.RoleViewer}

	t.Run("allows a change within 15 minutes of signing in", func(t *testing.T) {
		if _, err := h.service.CreateUser(t.Context(), h.authenticate(t, token), newUser, testClient()); err != nil {
			t.Errorf("CreateUser returned %v", err)
		}
	})

	h.clock.Advance(16 * time.Minute)

	t.Run("asks for a fresh check after 15 minutes", func(t *testing.T) {
		newUser.Username = "dave"
		if _, err := h.service.CreateUser(t.Context(), h.authenticate(t, token), newUser, testClient()); !errors.Is(err, auth.ErrStepUpRequired) {
			t.Errorf("CreateUser error = %v, want ErrStepUpRequired", err)
		}
	})

	t.Run("refuses a wrong step-up code", func(t *testing.T) {
		if err := h.service.StepUp(t.Context(), h.authenticate(t, token), "000000", testClient()); !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("StepUp error = %v, want ErrInvalidCode", err)
		}
	})

	t.Run("allows the change again after a step-up", func(t *testing.T) {
		if err := h.service.StepUp(t.Context(), h.authenticate(t, token), h.code(admin.totpSecret), testClient()); err != nil {
			t.Fatalf("StepUp returned %v", err)
		}
		if _, err := h.service.CreateUser(t.Context(), h.authenticate(t, token), newUser, testClient()); err != nil {
			t.Errorf("CreateUser after step-up returned %v", err)
		}
		if !contains(h.auditActions(t), "auth.step_up:success") {
			t.Errorf("audit = %v, want the step-up", h.auditActions(t))
		}
	})

	t.Run("RequireRecentSecondFactor applies the same window", func(t *testing.T) {
		if err := h.service.RequireRecentSecondFactor(h.authenticate(t, token)); err != nil {
			t.Errorf("right after step-up: %v", err)
		}
		h.clock.Advance(16 * time.Minute)
		if err := h.service.RequireRecentSecondFactor(h.authenticate(t, token)); !errors.Is(err, auth.ErrStepUpRequired) {
			t.Errorf("16 minutes later: %v, want ErrStepUpRequired", err)
		}
	})
}

func TestUserManagement(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, token, _ := h.setUpAdmin(t)
	admin := h.authenticate(t, token)

	t.Run("creates a user with a role and audits it", func(t *testing.T) {
		created, err := h.service.CreateUser(t.Context(), admin, auth.CreateUserInput{Username: "erin", Password: strongPassword, Role: auth.RoleOperator}, testClient())
		if err != nil || created.Username != "erin" || created.Role != auth.RoleOperator || created.ID == 0 {
			t.Errorf("CreateUser = %+v, %v; want erin as operator", created, err)
		}
		if !contains(h.auditActions(t), "user.create:success") {
			t.Errorf("audit = %v, want user.create", h.auditActions(t))
		}
	})

	t.Run("refuses a username that differs only in letter case", func(t *testing.T) {
		_, err := h.service.CreateUser(t.Context(), admin, auth.CreateUserInput{Username: "ERIN", Password: strongPassword, Role: auth.RoleViewer}, testClient())
		if !errors.Is(err, auth.ErrUsernameTaken) {
			t.Errorf("error = %v, want ErrUsernameTaken", err)
		}
	})

	t.Run("refuses an invalid username or weak password", func(t *testing.T) {
		inputs := []auth.CreateUserInput{
			{Username: "bad name", Password: strongPassword, Role: auth.RoleViewer},
			{Username: "harry", Password: "too short", Role: auth.RoleViewer},
		}
		for _, input := range inputs {
			var validation *auth.ValidationError
			if _, err := h.service.CreateUser(t.Context(), admin, input, testClient()); !errors.As(err, &validation) {
				t.Errorf("CreateUser(%+v) error = %v, want a validation error", input, err)
			}
		}
	})

	t.Run("refuses an unknown role", func(t *testing.T) {
		_, err := h.service.CreateUser(t.Context(), admin, auth.CreateUserInput{Username: "frank", Password: strongPassword, Role: "superuser"}, testClient())
		var validation *auth.ValidationError
		if !errors.As(err, &validation) || validation.Field != "role" {
			t.Errorf("error = %v, want a role validation error", err)
		}
	})

	t.Run("lists users", func(t *testing.T) {
		users, err := h.service.ListUsers(t.Context(), admin)
		if err != nil || len(users) != 2 || users[0].Username != "admin" || users[1].Username != "erin" {
			t.Errorf("ListUsers = %+v, %v; want admin and erin", users, err)
		}
	})

	t.Run("changes a role and audits it", func(t *testing.T) {
		erin := findUser(t, h, admin, "erin")
		viewer := auth.RoleViewer
		updated, err := h.service.UpdateUser(t.Context(), admin, erin.ID, auth.UpdateUserInput{Role: &viewer}, testClient())
		if err != nil || updated.Role != auth.RoleViewer {
			t.Errorf("UpdateUser = %+v, %v; want viewer", updated, err)
		}
		if !contains(h.auditActions(t), "user.role.change:success") {
			t.Errorf("audit = %v, want user.role.change", h.auditActions(t))
		}
	})

	t.Run("disabling a user ends their sessions and blocks login", func(t *testing.T) {
		grace := h.createUser(t, token, "grace", auth.RoleViewer)
		graceToken := h.signIn(t, grace)
		disabled := true
		if _, err := h.service.UpdateUser(t.Context(), admin, findUser(t, h, admin, "grace").ID, auth.UpdateUserInput{Disabled: &disabled}, testClient()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.Authenticate(t.Context(), graceToken); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("disabled user's session still works: %v", err)
		}
		if _, err := h.service.Login(t.Context(), auth.LoginInput{Username: "grace", Password: strongPassword}, auth.RequestInfo{IP: "192.0.2.9"}); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("disabled user login error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("re-enables a user", func(t *testing.T) {
		enabled := false
		updated, err := h.service.UpdateUser(t.Context(), admin, findUser(t, h, admin, "grace").ID, auth.UpdateUserInput{Disabled: &enabled}, testClient())
		if err != nil || updated.DisabledAt != nil {
			t.Errorf("UpdateUser = %+v, %v; want enabled", updated, err)
		}
	})

	t.Run("refuses to remove the last active admin", func(t *testing.T) {
		viewer := auth.RoleViewer
		if _, err := h.service.UpdateUser(t.Context(), admin, admin.UserID, auth.UpdateUserInput{Role: &viewer}, testClient()); !errors.Is(err, auth.ErrLastAdmin) {
			t.Errorf("demoting the last admin: %v, want ErrLastAdmin", err)
		}
		disabled := true
		if _, err := h.service.UpdateUser(t.Context(), admin, admin.UserID, auth.UpdateUserInput{Disabled: &disabled}, testClient()); !errors.Is(err, auth.ErrLastAdmin) {
			t.Errorf("disabling the last admin: %v, want ErrLastAdmin", err)
		}
	})

	t.Run("returns not found for an unknown user", func(t *testing.T) {
		viewer := auth.RoleViewer
		if _, err := h.service.UpdateUser(t.Context(), admin, 9999, auth.UpdateUserInput{Role: &viewer}, testClient()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("refuses an unknown role in an update", func(t *testing.T) {
		bad := auth.Role("root")
		var validation *auth.ValidationError
		if _, err := h.service.UpdateUser(t.Context(), admin, admin.UserID, auth.UpdateUserInput{Role: &bad}, testClient()); !errors.As(err, &validation) {
			t.Errorf("error = %v, want a validation error", err)
		}
	})
}

func TestRolesLimitUserManagement(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, adminToken, _ := h.setUpAdmin(t)

	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleOperator} {
		t.Run(string(role)+" can't list or create users", func(t *testing.T) {
			user := h.createUser(t, adminToken, "user-"+string(role), role)
			session := h.authenticate(t, h.signIn(t, user))

			if _, err := h.service.ListUsers(t.Context(), session); !errors.Is(err, auth.ErrForbidden) {
				t.Errorf("ListUsers error = %v, want ErrForbidden", err)
			}
			_, err := h.service.CreateUser(t.Context(), session, auth.CreateUserInput{Username: "x", Password: strongPassword, Role: auth.RoleAdmin}, testClient())
			if !errors.Is(err, auth.ErrForbidden) {
				t.Errorf("CreateUser error = %v, want ErrForbidden", err)
			}
			admin := auth.RoleAdmin
			if _, err := h.service.UpdateUser(t.Context(), session, session.UserID, auth.UpdateUserInput{Role: &admin}, testClient()); !errors.Is(err, auth.ErrForbidden) {
				t.Errorf("promoting oneself: %v, want ErrForbidden", err)
			}
		})
	}
}

func TestPasswordOnlySessionsCantStepUp(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	admin, _, _ := h.setUpAdmin(t)
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: admin.password}, testClient())
	if err != nil {
		t.Fatal(err)
	}
	pending := h.authenticate(t, result.SessionToken)

	if err := h.service.RequireRecentSecondFactor(pending); !errors.Is(err, auth.ErrNotAuthenticated) {
		t.Errorf("RequireRecentSecondFactor = %v, want ErrNotAuthenticated", err)
	}
	if err := h.service.StepUp(t.Context(), pending, h.code(admin.totpSecret), testClient()); !errors.Is(err, auth.ErrNotAuthenticated) {
		t.Errorf("StepUp = %v, want ErrNotAuthenticated", err)
	}
}

func findUser(t *testing.T, h *harness, admin auth.Session, username string) auth.User {
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

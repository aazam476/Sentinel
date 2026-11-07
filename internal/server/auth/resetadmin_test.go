// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

func TestSEC08_ResetAdminRestoresAccessWith2FAStillRequired(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	admin, oldToken, _ := h.setUpAdmin(t)
	// Lock the account, as a forgotten password would.
	for attempt := range 10 {
		h.clock.Advance(16 * time.Minute)
		_, _ = h.service.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: "wrong password!!"}, // failures expected
			auth.RequestInfo{IP: "198.51.100." + string(rune('0'+attempt))})
	}

	newPassword, err := h.restart(t).ResetAdmin(t.Context(), "")
	if err != nil {
		t.Fatalf("ResetAdmin returned %v", err)
	}

	t.Run("ends the admin's sessions", func(t *testing.T) {
		if _, err := h.service.Authenticate(t.Context(), oldToken); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("old session still works: %v", err)
		}
	})

	t.Run("the old password no longer works", func(t *testing.T) {
		_, err := h.restart(t).Login(t.Context(), auth.LoginInput{Username: admin.username, Password: admin.password}, auth.RequestInfo{IP: "192.0.2.1"})
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("Login error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("the new password works, unlocked, and leads to 2FA enrollment", func(t *testing.T) {
		result, err := h.restart(t).Login(t.Context(), auth.LoginInput{Username: admin.username, Password: newPassword}, auth.RequestInfo{IP: "192.0.2.2"})
		if err != nil {
			t.Fatalf("Login returned %v", err)
		}
		if result.NextStep != auth.NextStepEnrollSecondFactor {
			t.Errorf("next step = %q, want enroll_second_factor", result.NextStep)
		}
	})

	t.Run("is audited", func(t *testing.T) {
		if !contains(h.auditActions(t), "auth.admin.reset:success") {
			t.Errorf("audit = %v, want auth.admin.reset", h.auditActions(t))
		}
	})
}

func TestResetAdminChoosesTheAdmin(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, token, _ := h.setUpAdmin(t)
	h.createUser(t, token, "second-admin", auth.RoleAdmin)
	h.createUser(t, token, "viewer", auth.RoleViewer)

	tests := []struct {
		name     string
		username string
		wantErr  error
		wantText string
	}{
		{name: "needs a username when there are several admins", username: "", wantText: "several admins"},
		{name: "refuses a user who isn't an admin", username: "viewer", wantErr: auth.ErrNotAnAdmin},
		{name: "refuses an unknown user", username: "nobody", wantErr: auth.ErrNotFound},
		{name: "resets the named admin", username: "second-admin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			password, err := h.service.ResetAdmin(t.Context(), test.username)
			switch {
			case test.wantErr != nil:
				if !errors.Is(err, test.wantErr) {
					t.Errorf("error = %v, want %v", err, test.wantErr)
				}
			case test.wantText != "":
				if err == nil || !strings.Contains(err.Error(), test.wantText) {
					t.Errorf("error = %v, want one mentioning %q", err, test.wantText)
				}
			default:
				if err != nil || len(password) < 12 {
					t.Errorf("ResetAdmin = %q, %v; want a new password", password, err)
				}
			}
		})
	}
}

func TestResetAdminReportsDatabaseFailures(t *testing.T) {
	t.Parallel()
	for _, table := range []string{"users", "audit_log"} {
		t.Run("when writing "+table+" fails", func(t *testing.T) {
			h := newHarness(t)
			h.setUpAdmin(t)
			if table == "users" {
				h.failStatement(t, "UPDATE", table)
			} else {
				h.failStatement(t, "INSERT", table)
			}
			if _, err := h.service.ResetAdmin(t.Context(), "admin"); err == nil {
				t.Error("ResetAdmin returned nil")
			}
		})
	}

	t.Run("when deleting sessions fails", func(t *testing.T) {
		h := newHarness(t)
		h.setUpAdmin(t)
		h.failStatement(t, "DELETE", "sessions")
		if _, err := h.service.ResetAdmin(t.Context(), "admin"); err == nil {
			t.Error("ResetAdmin returned nil")
		}
	})

	t.Run("when looking up the named admin fails", func(t *testing.T) {
		h := newHarness(t)
		h.setUpAdmin(t)
		failReadOf("users")(t, h)
		if _, err := h.service.ResetAdmin(t.Context(), "admin"); err == nil {
			t.Error("ResetAdmin returned nil")
		}
	})

	t.Run("when the request is canceled before hashing", func(t *testing.T) {
		h := newHarness(t)
		h.setUpAdmin(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := h.service.ResetAdmin(ctx, "admin"); err == nil {
			t.Error("ResetAdmin returned nil")
		}
	})

	t.Run("when listing users fails", func(t *testing.T) {
		h := newHarness(t)
		h.setUpAdmin(t)
		failReadOf("users")(t, h)
		if _, err := h.service.ResetAdmin(t.Context(), ""); err == nil {
			t.Error("ResetAdmin returned nil")
		}
	})
}

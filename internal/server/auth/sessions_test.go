// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

func TestSEC06_SessionsExpire(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		passTime  func(t *testing.T, h *harness, token string)
		wantValid bool
	}{
		{
			name:      "stays valid after 11 hours idle",
			passTime:  func(t *testing.T, h *harness, token string) { h.clock.Advance(11 * time.Hour) },
			wantValid: true,
		},
		{
			name:      "ends after 12 hours idle",
			passTime:  func(t *testing.T, h *harness, token string) { h.clock.Advance(12*time.Hour + time.Minute) },
			wantValid: false,
		},
		{
			name: "ends 7 days after sign-in even when used all along",
			passTime: func(t *testing.T, h *harness, token string) {
				for elapsed := time.Duration(0); elapsed < 7*24*time.Hour; elapsed += 6 * time.Hour {
					h.clock.Advance(6 * time.Hour)
					_, _ = h.service.Authenticate(t.Context(), token) // keeps the session in use; the last call may fail
				}
				h.clock.Advance(time.Hour)
			},
			wantValid: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			_, token, _ := h.setUpAdmin(t)

			test.passTime(t, h, token)

			_, err := h.service.Authenticate(t.Context(), token)
			if test.wantValid && err != nil {
				t.Errorf("Authenticate returned %v, want a valid session", err)
			}
			if !test.wantValid && !errors.Is(err, auth.ErrNotAuthenticated) {
				t.Errorf("Authenticate error = %v, want ErrNotAuthenticated", err)
			}
		})
	}

	t.Run("a password-only session ends after 10 minutes", func(t *testing.T) {
		h := newHarness(t)
		admin, _, _ := h.setUpAdmin(t)
		result, err := h.service.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: admin.password}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		h.clock.Advance(11 * time.Minute)
		if _, err := h.service.Authenticate(t.Context(), result.SessionToken); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("Authenticate error = %v, want ErrNotAuthenticated", err)
		}
	})
}

func TestSEC06_UsersCanListAndRevokeTheirSessions(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	admin, setupToken, _ := h.setUpAdmin(t)
	otherToken := h.signIn(t, admin)
	current := h.authenticate(t, setupToken)

	sessions, err := h.service.ListSessions(t.Context(), current)
	if err != nil {
		t.Fatalf("ListSessions returned %v", err)
	}

	t.Run("lists every signed-in session and marks the current one", func(t *testing.T) {
		if len(sessions) != 2 {
			t.Fatalf("sessions = %+v, want 2", sessions)
		}
		currentCount := 0
		for _, session := range sessions {
			if session.Current {
				currentCount++
			}
			if session.IP != "203.0.113.7" || session.UserAgent != "test-browser" {
				t.Errorf("session = %+v, want the client's IP and user agent", session)
			}
		}
		if currentCount != 1 {
			t.Errorf("%d sessions marked current, want 1", currentCount)
		}
	})

	t.Run("revokes another session, which stops working at once", func(t *testing.T) {
		other := h.authenticate(t, otherToken)
		if err := h.service.RevokeSession(t.Context(), current, other.ID, testClient()); err != nil {
			t.Fatalf("RevokeSession returned %v", err)
		}
		if _, err := h.service.Authenticate(t.Context(), otherToken); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("revoked session still works: %v", err)
		}
		if !contains(h.auditActions(t), "auth.session.revoke:success") {
			t.Errorf("audit = %v, want the revocation", h.auditActions(t))
		}
	})

	t.Run("can't revoke another user's session", func(t *testing.T) {
		bob := h.createUser(t, setupToken, "bob", auth.RoleViewer)
		bobSession := h.authenticate(t, h.signIn(t, bob))
		if err := h.service.RevokeSession(t.Context(), current, bobSession.ID, testClient()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
		if err := h.service.RevokeSession(t.Context(), current, 9999, testClient()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("unknown session error = %v, want ErrNotFound", err)
		}
	})

	t.Run("logout ends the session", func(t *testing.T) {
		if err := h.service.Logout(t.Context(), current, testClient()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.Authenticate(t.Context(), setupToken); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("session still works after logout: %v", err)
		}
	})
}

func TestSEC06_SessionTokensAreStoredHashed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, token, _ := h.setUpAdmin(t)

	stored := h.queryString(t, `SELECT token_hash FROM sessions LIMIT 1`)

	if stored == token || strings.Contains(stored, token) || len(stored) != 64 {
		t.Errorf("stored token = %q, want a SHA-256 hex hash, not the token", stored)
	}
	if len(token) < 43 {
		t.Errorf("token %q is shorter than 256 bits of base64", token)
	}
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setUpAdmin(t)

	for _, token := range []string{"", "not-a-real-token"} {
		if _, err := h.service.Authenticate(t.Context(), token); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("Authenticate(%q) error = %v, want ErrNotAuthenticated", token, err)
		}
	}
}

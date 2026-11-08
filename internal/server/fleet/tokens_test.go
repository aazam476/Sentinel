// SPDX-License-Identifier: Apache-2.0

package fleet_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

func TestCreateEnrollmentToken(t *testing.T) {
	t.Run("creates a single-use token that lasts 24 hours by default", func(t *testing.T) {
		h := newHarness(t)
		token := h.newToken(t)

		now := h.clock.Now().UnixMilli()
		if token.Token == "" || token.MaxUses != 1 || token.ExpiresAt != now+(24*time.Hour).Milliseconds() {
			t.Errorf("token = %+v, want a secret, 1 use and 24 hours", token)
		}
		if token.Note != "office NAS" || token.CreatedBy != "alice" {
			t.Errorf("note/created by = %q/%q", token.Note, token.CreatedBy)
		}
		if got := h.lastAudit(t); got.Action != "enrollment_token.create" || got.Username != "alice" {
			t.Errorf("audit entry = %s by %q", got.Action, got.Username)
		}
	})

	t.Run("accepts a longer lifetime and several uses", func(t *testing.T) {
		h := newHarness(t)
		token, err := h.service.CreateEnrollmentToken(t.Context(), h.admin,
			fleet.CreateTokenInput{Lifetime: fleet.MaxTokenLifetime, MaxUses: 50}, browser())
		if err != nil {
			t.Fatal(err)
		}
		if token.MaxUses != 50 || token.ExpiresAt-token.CreatedAt != fleet.MaxTokenLifetime.Milliseconds() {
			t.Errorf("token = %+v", token)
		}
	})

	tests := []struct {
		name  string
		input fleet.CreateTokenInput
		field string
	}{
		{"refuses a lifetime over 7 days", fleet.CreateTokenInput{Lifetime: fleet.MaxTokenLifetime + time.Hour}, "expiresInHours"},
		{"refuses a lifetime under an hour", fleet.CreateTokenInput{Lifetime: time.Minute}, "expiresInHours"},
		{"refuses more than 1,000 uses", fleet.CreateTokenInput{MaxUses: fleet.MaxTokenUses + 1}, "maxUses"},
		{"refuses a negative number of uses", fleet.CreateTokenInput{MaxUses: -1}, "maxUses"},
		{"refuses a note over 100 characters", fleet.CreateTokenInput{Note: strings.Repeat("n", 101)}, "note"},
		{"refuses control characters in the note", fleet.CreateTokenInput{Note: "office\u202eSAN"}, "note"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			_, err := h.service.CreateEnrollmentToken(t.Context(), h.admin, test.input, browser())
			var validation *auth.ValidationError
			if !errors.As(err, &validation) || validation.Field != test.field {
				t.Errorf("err = %v, want a validation error for %s", err, test.field)
			}
		})
	}

	t.Run("only admins can create tokens", func(t *testing.T) {
		h := newHarness(t)
		for _, session := range []auth.Session{h.operator, h.viewer} {
			if _, err := h.service.CreateEnrollmentToken(t.Context(), session, fleet.CreateTokenInput{}, browser()); !errors.Is(err, auth.ErrForbidden) {
				t.Errorf("%s: err = %v, want ErrForbidden", session.Role, err)
			}
		}
	})
}

func TestListEnrollmentTokens(t *testing.T) {
	t.Run("lists live and recently ended tokens, newest first, without their secrets", func(t *testing.T) {
		h := newHarness(t)
		old := h.newToken(t)
		h.clock.Advance(8 * 24 * time.Hour) // old expired over a week ago
		first := h.newToken(t)
		h.clock.Advance(time.Minute)
		second := h.newToken(t)

		tokens, err := h.service.ListEnrollmentTokens(t.Context(), h.admin)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]int64, 0, len(tokens))
		for _, token := range tokens {
			ids = append(ids, token.ID)
		}
		if !slices.Equal(ids, []int64{second.ID, first.ID}) {
			t.Errorf("ids = %v, want %v (not %d)", ids, []int64{second.ID, first.ID}, old.ID)
		}
	})

	t.Run("only admins can list tokens", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.ListEnrollmentTokens(t.Context(), h.operator); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("err = %v, want ErrForbidden", err)
		}
	})
}

func TestRevokeEnrollmentToken(t *testing.T) {
	t.Run("a revoked token can't enroll and is audited once", func(t *testing.T) {
		h := newHarness(t)
		token := h.newToken(t)
		for range 2 {
			if err := h.service.RevokeEnrollmentToken(t.Context(), h.admin, token.ID, browser()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := h.service.Enroll(t.Context(), enrollInput(t, token.Token)); !errors.Is(err, fleet.ErrEnrollmentRefused) {
			t.Errorf("Enroll err = %v, want ErrEnrollmentRefused", err)
		}
		want := []string{"enrollment_token.create:success", "enrollment_token.revoke:success", "agent.enroll:failure"}
		if got := h.auditActions(t); !slices.Equal(got, want) {
			t.Errorf("audit = %v, want %v", got, want)
		}
	})

	t.Run("reports a token that doesn't exist", func(t *testing.T) {
		h := newHarness(t)
		if err := h.service.RevokeEnrollmentToken(t.Context(), h.admin, 99, browser()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("only admins can revoke tokens", func(t *testing.T) {
		h := newHarness(t)
		token := h.newToken(t)
		if err := h.service.RevokeEnrollmentToken(t.Context(), h.viewer, token.ID, browser()); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("err = %v, want ErrForbidden", err)
		}
	})
}

func TestTokenUsableAt(t *testing.T) {
	revoked := int64(5)
	tests := []struct {
		name  string
		token fleet.EnrollmentToken
		want  bool
	}{
		{"a fresh token is usable", fleet.EnrollmentToken{ExpiresAt: 100, MaxUses: 1}, true},
		{"an expired token is not", fleet.EnrollmentToken{ExpiresAt: 10, MaxUses: 1}, false},
		{"a used-up token is not", fleet.EnrollmentToken{ExpiresAt: 100, MaxUses: 2, UseCount: 2}, false},
		{"a revoked token is not", fleet.EnrollmentToken{ExpiresAt: 100, MaxUses: 1, RevokedAt: &revoked}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.token.UsableAt(10); got != test.want {
				t.Errorf("UsableAt = %v, want %v", got, test.want)
			}
		})
	}
}

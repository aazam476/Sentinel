// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"testing"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// optionalFixture is an optional-2FA server with an admin who uses TOTP
// and a user, ann, who doesn't.
type optionalFixture struct {
	admin auth.Session
	ann   auth.Session
}

func newOptionalFixture(t *testing.T, h *harness) optionalFixture {
	t.Helper()
	_, adminToken, _ := h.setUpAdmin(t)
	admin := h.authenticate(t, adminToken)
	if _, err := h.service.CreateUser(t.Context(), admin, auth.CreateUserInput{Username: "ann", Password: strongPassword, Role: auth.RoleViewer}, testClient()); err != nil {
		t.Fatal(err)
	}
	ann, err := h.loginStep(t, "ann")
	if err != nil {
		t.Fatal(err)
	}
	return optionalFixture{admin: h.authenticate(t, adminToken), ann: h.authenticate(t, ann.SessionToken)}
}

func TestTwoFactorDatabaseFailuresAreReported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		fault func(t *testing.T, h *harness)
		run   func(t *testing.T, h *harness, f optionalFixture) error
	}{
		{name: "reading the setting", fault: failReads, run: func(t *testing.T, h *harness, _ optionalFixture) error {
			_, err := h.service.TwoFactorRequired(t.Context())
			return err
		}},
		{name: "saving the setting", fault: failing("INSERT", "settings"), run: requireTwoFactor},
		{name: "signing out users without 2FA", fault: failing("DELETE", "sessions"), run: requireTwoFactor},
		{name: "auditing the setting", fault: failing("INSERT", "audit_log"), run: requireTwoFactor},
		{name: "the setting during login", fault: failReadOf("settings"), run: func(t *testing.T, h *harness, _ optionalFixture) error {
			_, err := h.loginStep(t, "ann")
			return err
		}},
		{name: "the setting when removing TOTP", fault: failReadOf("settings"), run: removeAdminTOTP},
		{name: "removing TOTP", fault: failing("DELETE", "totp_secrets"), run: removeAdminTOTP},
		{name: "removing recovery codes", fault: failing("DELETE", "recovery_codes"), run: removeAdminTOTP},
		{name: "auditing a removed TOTP", fault: failing("INSERT", "audit_log"), run: removeAdminTOTP},
		{name: "the user for a password step-up", fault: failReads, run: stepUpAnn},
		{name: "the session after a password step-up", fault: failing("UPDATE", "sessions"), run: stepUpAnn},
		{name: "auditing a password step-up", fault: failing("INSERT", "audit_log"), run: stepUpAnn},
	}
	for _, test := range tests {
		t.Run("reports a failure in "+test.name, func(t *testing.T) {
			t.Parallel()
			h := newOptionalHarness(t)
			f := newOptionalFixture(t, h)
			test.fault(t, h)
			if err := test.run(t, h, f); err == nil {
				t.Error("returned nil, want the database failure")
			}
		})
	}
}

func requireTwoFactor(t *testing.T, h *harness, f optionalFixture) error {
	return h.service.SetTwoFactorRequired(t.Context(), f.admin, true, testClient())
}

func removeAdminTOTP(t *testing.T, h *harness, f optionalFixture) error {
	return h.service.RemoveTOTP(t.Context(), f.admin, testClient())
}

func stepUpAnn(t *testing.T, h *harness, f optionalFixture) error {
	return h.service.StepUpWithPassword(t.Context(), f.ann, strongPassword, testClient())
}

func TestTwoFactorRulesHoldForEveryKindOfSession(t *testing.T) {
	h := newOptionalHarness(t)
	f := newOptionalFixture(t, h)
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "a step-up without a session", err: h.service.StepUpWithPassword(t.Context(), auth.Session{}, strongPassword, testClient()), want: auth.ErrNotAuthenticated},
		{name: "removing TOTP without a recent check", err: h.service.RemoveTOTP(t.Context(), auth.Session{}, testClient()), want: auth.ErrNotAuthenticated},
		{name: "removing TOTP a user hasn't got", err: h.service.RemoveTOTP(t.Context(), f.ann, testClient()), want: auth.ErrNotFound},
		{name: "requiring 2FA without a recent check", err: h.service.SetTwoFactorRequired(t.Context(), auth.Session{Role: auth.RoleAdmin}, true, testClient()), want: auth.ErrNotAuthenticated},
	}
	for _, test := range tests {
		t.Run("refuses "+test.name, func(t *testing.T) {
			if !errors.Is(test.err, test.want) {
				t.Errorf("err = %v, want %v", test.err, test.want)
			}
		})
	}
}

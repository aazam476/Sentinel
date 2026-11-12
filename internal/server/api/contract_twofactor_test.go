// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/auth"
)

// setUpAdminWithout2FA runs setup over HTTP, skipping 2FA.
func (s *testServer) setUpAdminWithout2FA() *browser {
	s.t.Helper()
	browser := s.newBrowser()
	begun := browser.do(http.MethodPost, "/api/setup/begin", api.BeginSetupRequest{Username: "admin", Password: strongPassword})
	var setup api.BeginSetupResponse
	begun.decode(s.t, &setup)
	completed := browser.do(http.MethodPost, "/api/setup/complete", api.CompleteSetupRequest{SetupID: setup.SetupID, SkipTwoFactor: true})
	expect(s.t, completed, http.StatusOK, "")
	var signedIn api.SignedInResponse
	completed.decode(s.t, &signedIn)
	if signedIn.Session.State != "signed_in" || signedIn.Session.SecondFactors.TOTP || len(signedIn.RecoveryCodes) != 0 {
		s.t.Fatalf("setup without 2FA = %+v", signedIn)
	}
	return browser
}

func TestSEC04_TwoFactorIsOptionalOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdminWithout2FA()

	t.Run("reports that 2FA isn't required", func(t *testing.T) {
		var settings api.SecuritySettings
		got := admin.do(http.MethodGet, "/api/settings/security", nil)
		got.decode(t, &settings)
		if got.status != http.StatusOK || settings.RequireTwoFactor {
			t.Errorf("settings = %d %+v", got.status, settings)
		}
	})

	t.Run("an admin without 2FA can't require it", func(t *testing.T) {
		got := admin.do(http.MethodPut, "/api/settings/security", api.SecuritySettings{RequireTwoFactor: true})
		expect(t, got, http.StatusConflict, "own_second_factor_needed")
	})

	t.Run("a password signs in a user without 2FA", func(t *testing.T) {
		expect(t, admin.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: "bob", Password: strongPassword, Role: "viewer"}), http.StatusCreated, "")
		bob := server.newBrowser()
		var login api.LoginResponse
		bob.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "bob", Password: strongPassword}).decode(t, &login)
		if login.NextStep != "signed_in" {
			t.Errorf("next step = %q, want signed_in", login.NextStep)
		}
		expect(t, bob.do(http.MethodPut, "/api/settings/security", api.SecuritySettings{RequireTwoFactor: false}), http.StatusForbidden, "forbidden")
		var users api.UsersResponse
		admin.do(http.MethodGet, "/api/users", nil).decode(t, &users)
		for _, user := range users.Users {
			if user.TwoFactor {
				t.Errorf("%s shows 2FA it hasn't got", user.Username)
			}
		}
	})

	t.Run("SEC-07 step-up takes the password", func(t *testing.T) {
		server.clock.Advance(auth.StepUpWindow + time.Minute)
		expect(t, admin.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: "cy", Password: strongPassword, Role: "viewer"}), http.StatusForbidden, "step_up_required")
		expect(t, admin.do(http.MethodPost, "/api/auth/step-up", api.StepUpRequest{Password: "not the password"}), http.StatusUnauthorized, "invalid_credentials")
		expect(t, admin.do(http.MethodPost, "/api/auth/step-up", api.StepUpRequest{Password: strongPassword}), http.StatusOK, "")
		expect(t, admin.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: "cy", Password: strongPassword, Role: "viewer"}), http.StatusCreated, "")
	})

	t.Run("adds and removes an authenticator app from the account", func(t *testing.T) {
		started := admin.do(http.MethodPost, "/api/auth/totp/enrollment", nil)
		expect(t, started, http.StatusOK, "")
		var enrollment api.TOTPEnrollmentResponse
		started.decode(t, &enrollment)
		confirmed := admin.do(http.MethodPost, "/api/auth/totp/enrollment/confirm", api.TOTPCodeRequest{TOTPCode: server.code(enrollment.Secret)})
		expect(t, confirmed, http.StatusOK, "")
		var signedIn api.SignedInResponse
		confirmed.decode(t, &signedIn)
		if !signedIn.Session.SecondFactors.TOTP || len(signedIn.RecoveryCodes) != 10 {
			t.Errorf("after adding = %+v", signedIn)
		}
		expect(t, admin.do(http.MethodDelete, "/api/auth/totp", nil), http.StatusNoContent, "")
		expect(t, admin.do(http.MethodDelete, "/api/auth/totp", nil), http.StatusNotFound, "not_found")
	})
}

func TestSEC04_RequiringTwoFactorOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	admin.requireTwoFactor(t)
	var settings api.SecuritySettings
	admin.browser.do(http.MethodGet, "/api/settings/security", nil).decode(t, &settings)
	if !settings.RequireTwoFactor {
		t.Error("not required after turning it on")
	}
	expect(t, admin.browser.do(http.MethodDelete, "/api/auth/totp", nil), http.StatusConflict, "last_second_factor")
}

func TestSetupStillChecksTheCodeUnlessSkipping(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	browser := server.newBrowser()
	begun := browser.do(http.MethodPost, "/api/setup/begin", api.BeginSetupRequest{Username: "admin", Password: strongPassword})
	var setup api.BeginSetupResponse
	begun.decode(t, &setup)
	got := browser.do(http.MethodPost, "/api/setup/complete", api.CompleteSetupRequest{SetupID: setup.SetupID, TOTPCode: "000000"})
	expect(t, got, http.StatusUnauthorized, "invalid_code")
}

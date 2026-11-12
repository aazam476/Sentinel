// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/api"
)

func TestSEC04_SignInOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	browser := server.newBrowser()

	t.Run("an anonymous browser is not signed in", func(t *testing.T) {
		expect(t, browser.do(http.MethodGet, "/api/auth/session", nil), http.StatusUnauthorized, "not_authenticated")
	})

	t.Run("a wrong password is refused", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "admin", Password: "wrong password!!"})
		expect(t, got, http.StatusUnauthorized, "invalid_credentials")
	})

	t.Run("a right password leads to the second-factor step", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "admin", Password: strongPassword})
		expect(t, got, http.StatusOK, "")
		var login api.LoginResponse
		got.decode(t, &login)
		if login.NextStep != "second_factor" {
			t.Errorf("next step = %q, want second_factor", login.NextStep)
		}
		var session api.SessionResponse
		browser.do(http.MethodGet, "/api/auth/session", nil).decode(t, &session)
		if session.State != "second_factor" || session.StepUpValidUntil != nil {
			t.Errorf("session = %+v, want state second_factor", session)
		}
	})

	t.Run("the password step alone opens nothing", func(t *testing.T) {
		expect(t, browser.do(http.MethodGet, "/api/users", nil), http.StatusUnauthorized, "not_authenticated")
		expect(t, browser.do(http.MethodGet, "/api/auth/sessions", nil), http.StatusUnauthorized, "not_authenticated")
	})

	t.Run("a wrong code is refused", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/auth/login/second-factor", api.SecondFactorRequest{TOTPCode: "000000"})
		expect(t, got, http.StatusUnauthorized, "invalid_code")
	})

	t.Run("a right code signs in", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/auth/login/second-factor", api.SecondFactorRequest{TOTPCode: server.code(admin.totpSecret)})
		expect(t, got, http.StatusOK, "")
		var signedIn api.SignedInResponse
		got.decode(t, &signedIn)
		if signedIn.Session.State != "signed_in" || signedIn.Session.StepUpValidUntil == nil {
			t.Errorf("response = %+v, want signed in with a step-up window", signedIn)
		}
		assertSessionCookie(t, got)
	})

	t.Run("a signed-in browser can't repeat the second-factor step", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/auth/login/second-factor", api.SecondFactorRequest{TOTPCode: "123456"})
		expect(t, got, http.StatusConflict, "wrong_login_step")
	})

	t.Run("logout ends the session and clears the cookie", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/auth/logout", nil)
		expect(t, got, http.StatusNoContent, "")
		expect(t, browser.do(http.MethodGet, "/api/auth/session", nil), http.StatusUnauthorized, "not_authenticated")
	})
}

func TestSEC04_RecoveryCodeSignInOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	browser := server.newBrowser()
	expect(t, browser.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "admin", Password: strongPassword}), http.StatusOK, "")

	got := browser.do(http.MethodPost, "/api/auth/login/second-factor", api.SecondFactorRequest{RecoveryCode: admin.recoveryCodes[0]})

	expect(t, got, http.StatusOK, "")
	var signedIn api.SignedInResponse
	got.decode(t, &signedIn)
	if signedIn.RemainingRecoveryCodes == nil || *signedIn.RemainingRecoveryCodes != 9 {
		t.Errorf("remaining = %v, want 9", signedIn.RemainingRecoveryCodes)
	}
}

func TestSEC04_NewUsersEnrollOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	admin.requireTwoFactor(t)
	expect(t, admin.browser.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: "bob", Password: strongPassword, Role: "viewer"}), http.StatusCreated, "")
	bob := server.newBrowser()

	got := bob.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "bob", Password: strongPassword})
	var login api.LoginResponse
	got.decode(t, &login)
	if login.NextStep != "enroll_second_factor" {
		t.Fatalf("next step = %q, want enroll_second_factor", login.NextStep)
	}
	var session api.SessionResponse
	bob.do(http.MethodGet, "/api/auth/session", nil).decode(t, &session)
	if session.State != "enroll_second_factor" {
		t.Errorf("session state = %q, want enroll_second_factor", session.State)
	}

	started := bob.do(http.MethodPost, "/api/auth/totp/enrollment", nil)
	expect(t, started, http.StatusOK, "")
	var enrollment api.TOTPEnrollmentResponse
	started.decode(t, &enrollment)
	expect(t, bob.do(http.MethodPost, "/api/auth/totp/enrollment/confirm", api.TOTPCodeRequest{TOTPCode: "000000"}), http.StatusUnauthorized, "invalid_code")

	confirmed := bob.do(http.MethodPost, "/api/auth/totp/enrollment/confirm", api.TOTPCodeRequest{TOTPCode: server.code(enrollment.Secret)})
	expect(t, confirmed, http.StatusOK, "")
	var signedIn api.SignedInResponse
	confirmed.decode(t, &signedIn)
	if signedIn.Session.State != "signed_in" || len(signedIn.RecoveryCodes) != 10 {
		t.Errorf("response = %+v, want signed in with 10 recovery codes", signedIn)
	}
	expect(t, bob.do(http.MethodPost, "/api/auth/totp/enrollment", nil), http.StatusConflict, "wrong_login_step")
}

func TestSEC05_RateLimitingOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	server.setUpAdmin()
	browser := server.newBrowser()
	wrong := api.LoginRequest{Username: "admin", Password: "wrong password!!"}
	for range 3 {
		expect(t, browser.do(http.MethodPost, "/api/auth/login", wrong), http.StatusUnauthorized, "invalid_credentials")
	}

	got := browser.do(http.MethodPost, "/api/auth/login", wrong)

	expect(t, got, http.StatusTooManyRequests, "too_many_attempts")
	if retryAfter, err := strconv.Atoi(got.header.Get("Retry-After")); err != nil || retryAfter < 1 {
		t.Errorf("Retry-After = %q, want a positive number of seconds", got.header.Get("Retry-After"))
	}
	var body api.ErrorResponse
	got.decode(t, &body)
	if body.Error.RetryAfterSeconds != 1 {
		t.Errorf("retryAfterSeconds = %d, want 1", body.Error.RetryAfterSeconds)
	}
}

func TestSEC06_SessionsOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	second := server.newBrowser()
	expect(t, second.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "admin", Password: strongPassword}), http.StatusOK, "")
	expect(t, second.do(http.MethodPost, "/api/auth/login/second-factor", api.SecondFactorRequest{TOTPCode: server.code(admin.totpSecret)}), http.StatusOK, "")

	var listed api.SessionsResponse
	admin.browser.do(http.MethodGet, "/api/auth/sessions", nil).decode(t, &listed)
	if len(listed.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want 2", listed.Sessions)
	}
	var other api.SessionSummary
	for _, session := range listed.Sessions {
		if !session.Current {
			other = session
		}
	}

	t.Run("revokes another session", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodDelete, "/api/auth/sessions/"+strconv.FormatInt(other.ID, 10), nil), http.StatusNoContent, "")
		expect(t, second.do(http.MethodGet, "/api/auth/session", nil), http.StatusUnauthorized, "not_authenticated")
	})

	t.Run("returns not found for an unknown or malformed session ID", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodDelete, "/api/auth/sessions/99999", nil), http.StatusNotFound, "not_found")
		expect(t, admin.browser.do(http.MethodDelete, "/api/auth/sessions/abc", nil), http.StatusNotFound, "not_found")
	})

	t.Run("revoking the current session signs this browser out", func(t *testing.T) {
		var mine api.SessionsResponse
		admin.browser.do(http.MethodGet, "/api/auth/sessions", nil).decode(t, &mine)
		expect(t, admin.browser.do(http.MethodDelete, "/api/auth/sessions/"+strconv.FormatInt(mine.Sessions[0].ID, 10), nil), http.StatusNoContent, "")
		expect(t, admin.browser.do(http.MethodGet, "/api/auth/session", nil), http.StatusUnauthorized, "not_authenticated")
	})

	t.Run("a session ends after 12 hours idle", func(t *testing.T) {
		fresh := server.newBrowser()
		expect(t, fresh.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "admin", Password: strongPassword}), http.StatusOK, "")
		server.clock.Advance(30 * time.Second)
		expect(t, fresh.do(http.MethodPost, "/api/auth/login/second-factor", api.SecondFactorRequest{TOTPCode: server.code(admin.totpSecret)}), http.StatusOK, "")
		server.clock.Advance(13 * time.Hour)
		expect(t, fresh.do(http.MethodGet, "/api/auth/session", nil), http.StatusUnauthorized, "not_authenticated")
	})
}

func TestSEC07_StepUpOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	server.clock.Advance(16 * time.Minute)
	newUser := api.CreateUserRequest{Username: "carol", Password: strongPassword, Role: "viewer"}

	t.Run("a user change needs a fresh second factor", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodPost, "/api/users", newUser), http.StatusForbidden, "step_up_required")
	})

	t.Run("a wrong step-up code is refused", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodPost, "/api/auth/step-up", api.TOTPCodeRequest{TOTPCode: "000000"}), http.StatusUnauthorized, "invalid_code")
	})

	t.Run("after a step-up the change goes through", func(t *testing.T) {
		got := admin.browser.do(http.MethodPost, "/api/auth/step-up", api.TOTPCodeRequest{TOTPCode: server.code(admin.totpSecret)})
		expect(t, got, http.StatusOK, "")
		var session api.SessionResponse
		got.decode(t, &session)
		if session.StepUpValidUntil == nil || *session.StepUpValidUntil != server.clock.Now().Add(15*time.Minute).UnixMilli() {
			t.Errorf("stepUpValidUntil = %v, want 15 minutes from now", session.StepUpValidUntil)
		}
		expect(t, admin.browser.do(http.MethodPost, "/api/users", newUser), http.StatusCreated, "")
	})
}

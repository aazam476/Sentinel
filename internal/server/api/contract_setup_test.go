// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/api"
)

func TestSEC02_SetupOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	browser := server.newBrowser()

	t.Run("reports that setup is required", func(t *testing.T) {
		got := browser.do(http.MethodGet, "/api/setup/status", nil)
		var status api.SetupStatusResponse
		got.decode(t, &status)
		if got.status != http.StatusOK || !status.SetupRequired {
			t.Errorf("status = %d %+v, want setupRequired", got.status, status)
		}
	})

	t.Run("refuses a weak password and names the field", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/setup/begin", api.BeginSetupRequest{Username: "admin", Password: "password1234"})
		expect(t, got, http.StatusBadRequest, "invalid_input")
		var body api.ErrorResponse
		got.decode(t, &body)
		if body.Error.Field != "password" {
			t.Errorf("field = %q, want password", body.Error.Field)
		}
	})

	var setup api.BeginSetupResponse
	t.Run("returns a TOTP secret and QR code", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/setup/begin", api.BeginSetupRequest{Username: "admin", Password: strongPassword})
		expect(t, got, http.StatusOK, "")
		got.decode(t, &setup)
		if setup.SetupID == "" || setup.TOTP.Secret == "" || setup.TOTP.QRCode == "" {
			t.Errorf("setup = %+v, want an ID, secret and QR code", setup)
		}
	})

	t.Run("refuses a wrong TOTP code", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/setup/complete", api.CompleteSetupRequest{SetupID: setup.SetupID, TOTPCode: "000000"})
		expect(t, got, http.StatusUnauthorized, "invalid_code")
	})

	t.Run("refuses an unknown setup ID", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/setup/complete", api.CompleteSetupRequest{SetupID: "nope", TOTPCode: "000000"})
		expect(t, got, http.StatusGone, "setup_expired")
	})

	t.Run("creates the admin, signs them in and returns recovery codes", func(t *testing.T) {
		got := browser.do(http.MethodPost, "/api/setup/complete", api.CompleteSetupRequest{
			SetupID: setup.SetupID, TOTPCode: server.code(setup.TOTP.Secret),
		})
		expect(t, got, http.StatusOK, "")
		var signedIn api.SignedInResponse
		got.decode(t, &signedIn)
		if signedIn.Session.State != "signed_in" || signedIn.Session.User.Role != "admin" || len(signedIn.RecoveryCodes) != 10 {
			t.Errorf("response = %+v, want a signed-in admin with 10 recovery codes", signedIn)
		}
		assertSessionCookie(t, got)
	})

	t.Run("closes setup afterwards", func(t *testing.T) {
		var status api.SetupStatusResponse
		browser.do(http.MethodGet, "/api/setup/status", nil).decode(t, &status)
		if status.SetupRequired {
			t.Error("setup is still required")
		}
		got := browser.do(http.MethodPost, "/api/setup/begin", api.BeginSetupRequest{Username: "x", Password: strongPassword})
		expect(t, got, http.StatusConflict, "setup_not_available")
	})
}

// assertSessionCookie checks the session cookie's protections (SEC-06,
// SEC-12).
func assertSessionCookie(t *testing.T, got response) {
	t.Helper()
	cookies := (&http.Response{Header: got.header}).Cookies()
	for _, cookie := range cookies {
		if cookie.Name != "sentinel_session" {
			continue
		}
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
			t.Errorf("cookie = %+v, want HttpOnly, Secure, SameSite=Strict, Path=/", cookie)
		}
		if cookie.MaxAge != int((7 * 24 * time.Hour).Seconds()) {
			t.Errorf("cookie Max-Age = %d, want 7 days", cookie.MaxAge)
		}
		return
	}
	t.Errorf("no sentinel_session cookie in %v", got.header)
}

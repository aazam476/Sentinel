// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/config"
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

// beginSetupFrom starts setup with the given Host, Origin and forwarded
// client address.
func beginSetupFrom(t *testing.T, server *testServer, host, forwardedFor string) response {
	t.Helper()
	body := strings.NewReader(`{"username":"admin","password":"` + strongPassword + `"}`)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.server.URL+"/api/setup/begin", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = host
	request.Header.Set("Origin", "https://"+host)
	request.Header.Set("Content-Type", "application/json")
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	return server.newBrowser().send(request)
}

func TestSEC01_SetupOnlyFromWhereItCantBeATakeover(t *testing.T) {
	t.Parallel()
	viaProxy := func(allowed ...string) func(*config.File) {
		return func(file *config.File) {
			file.TrustedProxies = []string{"127.0.0.1"}
			file.SetupAllowedFrom = allowed
		}
	}

	t.Run("refuses setup from the internet", func(t *testing.T) {
		server := newTestServerWith(t, viaProxy())
		host := strings.TrimPrefix(server.publicOrigin, "https://")
		expect(t, beginSetupFrom(t, server, host, "203.0.113.9"), http.StatusForbidden, "setup_not_allowed_here")
	})

	t.Run("allows it from a private network", func(t *testing.T) {
		server := newTestServerWith(t, viaProxy())
		host := strings.TrimPrefix(server.publicOrigin, "https://")
		expect(t, beginSetupFrom(t, server, host, "192.168.1.30"), http.StatusOK, "")
	})

	t.Run("allows it from wherever setup_allowed_from says", func(t *testing.T) {
		server := newTestServerWith(t, viaProxy("203.0.113.0/24"))
		host := strings.TrimPrefix(server.publicOrigin, "https://")
		expect(t, beginSetupFrom(t, server, host, "203.0.113.9"), http.StatusOK, "")
	})

	withoutPublicURL := func(file *config.File) { file.PublicURL = "" }

	t.Run("without public_url, refuses a browser that came by a public name (DNS rebinding)", func(t *testing.T) {
		server := newTestServerWith(t, withoutPublicURL)
		expect(t, beginSetupFrom(t, server, "sentinel.attacker.example", ""), http.StatusForbidden, "setup_not_allowed_here")
	})

	t.Run("without public_url, allows an address or a local name", func(t *testing.T) {
		for _, host := range []string{"127.0.0.1:7443", "[::1]:7443", "localhost:7443", "nas:7443", "nas.local", "sentinel.home.arpa"} {
			server := newTestServerWith(t, withoutPublicURL)
			if got := beginSetupFrom(t, server, host, ""); got.status != http.StatusOK {
				t.Errorf("Host %s: %d %s", host, got.status, got.body)
			}
		}
	})
}

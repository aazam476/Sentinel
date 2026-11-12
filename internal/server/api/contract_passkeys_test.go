// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/testutil/softauthn"
)

// passkeyCeremony runs begin and finish for a registration or assertion,
// with answer producing the browser's credential from the options.
func passkeyCeremony(t *testing.T, browser *browser, kind, name string, answer func(options []byte) ([]byte, error)) response {
	t.Helper()
	begun := browser.do(http.MethodPost, "/api/auth/passkeys/"+kind+"/begin", nil)
	expect(t, begun, http.StatusOK, "")
	var options api.PasskeyOptionsResponse
	begun.decode(t, &options)
	credential, err := answer(options.Options)
	if err != nil {
		t.Fatal(err)
	}
	return browser.do(http.MethodPost, "/api/auth/passkeys/"+kind+"/finish", api.FinishPasskeyRequest{Name: name, Credential: json.RawMessage(credential)})
}

func TestSEC04_PasskeysOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	admin.requireTwoFactor(t)
	expect(t, admin.browser.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: "pat", Password: strongPassword, Role: "operator"}), http.StatusCreated, "")
	authenticator := softauthn.New(server.publicOrigin)
	pat := server.newBrowser()
	expect(t, pat.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "pat", Password: strongPassword}), http.StatusOK, "")

	t.Run("a new user can choose a passkey as their second factor", func(t *testing.T) {
		got := passkeyCeremony(t, pat, "registration", "Laptop", authenticator.Register)
		expect(t, got, http.StatusOK, "")
		var signedIn api.SignedInResponse
		got.decode(t, &signedIn)
		if signedIn.Session.State != "signed_in" || !signedIn.Session.SecondFactors.Passkey || signedIn.Session.SecondFactors.TOTP || len(signedIn.RecoveryCodes) != 10 {
			t.Errorf("response = %+v, want signed in with a passkey and 10 recovery codes", signedIn)
		}
	})

	t.Run("the passkey finishes a later sign-in", func(t *testing.T) {
		browser := server.newBrowser()
		expect(t, browser.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "pat", Password: strongPassword}), http.StatusOK, "")
		var pending api.SessionResponse
		browser.do(http.MethodGet, "/api/auth/session", nil).decode(t, &pending)
		if pending.State != "second_factor" || !pending.SecondFactors.Passkey {
			t.Errorf("pending session = %+v, want the passkey offered", pending)
		}
		got := passkeyCeremony(t, browser, "assertion", "", authenticator.Sign)
		expect(t, got, http.StatusOK, "")
		var signedIn api.SignedInResponse
		got.decode(t, &signedIn)
		if signedIn.Session.State != "signed_in" {
			t.Errorf("state = %q, want signed_in", signedIn.Session.State)
		}
	})

	t.Run("an answer from another site is refused", func(t *testing.T) {
		browser := server.newBrowser()
		expect(t, browser.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "pat", Password: strongPassword}), http.StatusOK, "")
		phishing := *authenticator
		phishing.Origin = "https://evil.example.com"
		expect(t, passkeyCeremony(t, browser, "assertion", "", phishing.Sign), http.StatusUnauthorized, "passkey_failed")
	})

	t.Run("the passkey works for step-up", func(t *testing.T) {
		server.clock.Advance(16 * time.Minute)
		got := passkeyCeremony(t, pat, "assertion", "", authenticator.Sign)
		expect(t, got, http.StatusOK, "")
		var session api.SessionResponse
		got.decode(t, &session)
		if session.StepUpValidUntil == nil || *session.StepUpValidUntil != server.clock.Now().Add(15*time.Minute).UnixMilli() {
			t.Errorf("stepUpValidUntil = %v, want 15 minutes from now", session.StepUpValidUntil)
		}
	})

	t.Run("lists passkeys and refuses to remove the only second factor", func(t *testing.T) {
		var listed api.PasskeysResponse
		got := pat.do(http.MethodGet, "/api/auth/passkeys", nil)
		expect(t, got, http.StatusOK, "")
		got.decode(t, &listed)
		if len(listed.Passkeys) != 1 || listed.Passkeys[0].Name != "Laptop" || listed.Passkeys[0].LastUsedAt == nil {
			t.Fatalf("passkeys = %+v, want the used Laptop passkey", listed.Passkeys)
		}
		id := strconv.FormatInt(listed.Passkeys[0].ID, 10)
		expect(t, pat.do(http.MethodDelete, "/api/auth/passkeys/"+id, nil), http.StatusConflict, "last_second_factor")
	})

	t.Run("a signed-in user adds and removes a second passkey", func(t *testing.T) {
		expect(t, passkeyCeremony(t, pat, "registration", "Phone", softauthn.New(server.publicOrigin).Register), http.StatusNoContent, "")
		var listed api.PasskeysResponse
		pat.do(http.MethodGet, "/api/auth/passkeys", nil).decode(t, &listed)
		if len(listed.Passkeys) != 2 {
			t.Fatalf("passkeys = %+v, want 2", listed.Passkeys)
		}
		expect(t, pat.do(http.MethodDelete, "/api/auth/passkeys/"+strconv.FormatInt(listed.Passkeys[1].ID, 10), nil), http.StatusNoContent, "")
		expect(t, pat.do(http.MethodDelete, "/api/auth/passkeys/x", nil), http.StatusNotFound, "not_found")
	})

	t.Run("a TOTP user without passkeys can't start a passkey check", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodPost, "/api/auth/passkeys/assertion/begin", nil), http.StatusConflict, "wrong_login_step")
	})

	t.Run("an anonymous browser can't start a passkey ceremony", func(t *testing.T) {
		expect(t, server.newBrowser().do(http.MethodPost, "/api/auth/passkeys/registration/begin", nil), http.StatusUnauthorized, "not_authenticated")
	})

	t.Run("finishing with a malformed request is refused", func(t *testing.T) {
		expect(t, pat.do(http.MethodPost, "/api/auth/passkeys/assertion/finish", map[string]any{"credential": 5, "extra": true}), http.StatusBadRequest, "invalid_json")
		expect(t, pat.do(http.MethodPost, "/api/auth/passkeys/registration/finish", map[string]any{"credential": map[string]any{}}), http.StatusUnauthorized, "passkey_failed")
	})
}

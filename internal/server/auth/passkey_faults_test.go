// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/testutil/softauthn"
)

// passkeyFixture is a signed-in user ("pat") whose only second factor is a
// passkey.
type passkeyFixture struct {
	adminToken    string
	authenticator *softauthn.Authenticator
	patToken      string
}

func newPasskeyFixture(t *testing.T, h *harness) passkeyFixture {
	t.Helper()
	_, adminToken, _ := h.setUpAdmin(t)
	authenticator, signedIn := enrollWithPasskey(t, h, adminToken)
	return passkeyFixture{adminToken: adminToken, authenticator: authenticator, patToken: signedIn.SessionToken}
}

// patPending returns a fresh password-only session for pat.
func (f passkeyFixture) patPending(t *testing.T, h *harness) auth.Session {
	t.Helper()
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: "pat", Password: strongPassword}, auth.RequestInfo{IP: "192.0.2.90"})
	if err != nil {
		t.Fatal(err)
	}
	return h.authenticate(t, result.SessionToken)
}

func TestPasskeyEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("refuses a site without a host name", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		if _, err := h.service.BeginPasskeyAssertion(t.Context(), h.authenticate(t, f.patToken), auth.RelyingParty{ID: "not a host name!", Origin: "https://localhost:7443"}); err == nil {
			t.Error("BeginPasskeyAssertion accepted an empty relying party")
		}
		if _, err := h.service.BeginPasskeyRegistration(t.Context(), h.authenticate(t, f.patToken), auth.RelyingParty{ID: "not a host name!", Origin: "https://localhost:7443"}); err == nil {
			t.Error("BeginPasskeyRegistration accepted an empty relying party")
		}
	})

	t.Run("finishing with a site without a host name fails", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		session := h.authenticate(t, f.patToken)
		options, err := h.service.BeginPasskeyRegistration(t.Context(), session, testSite())
		if err != nil {
			t.Fatal(err)
		}
		registration, _ := softauthn.New(testSite().Origin).Register(options) // valid options can't fail
		if _, err := h.service.FinishPasskeyRegistration(t.Context(), session, auth.RelyingParty{ID: "not a host name!", Origin: "https://localhost:7443"}, "", registration, testClient()); err == nil {
			t.Error("FinishPasskeyRegistration accepted an empty relying party")
		}

		options, err = h.service.BeginPasskeyAssertion(t.Context(), session, testSite())
		if err != nil {
			t.Fatal(err)
		}
		assertion, _ := f.authenticator.Sign(options) // valid options can't fail
		if _, err := h.service.FinishPasskeyAssertion(t.Context(), session, auth.RelyingParty{ID: "not a host name!", Origin: "https://localhost:7443"}, assertion, testClient()); err == nil {
			t.Error("FinishPasskeyAssertion accepted an empty relying party")
		}
	})

	t.Run("a password-only session of a user with 2FA can't register a passkey", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		if _, err := h.service.BeginPasskeyRegistration(t.Context(), f.patPending(t, h), testSite()); !errors.Is(err, auth.ErrWrongLoginStep) {
			t.Errorf("error = %v, want ErrWrongLoginStep", err)
		}
	})

	t.Run("finishing a registration that never started fails", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		_, err := h.service.FinishPasskeyRegistration(t.Context(), h.authenticate(t, f.patToken), testSite(), "", []byte("{}"), testClient())
		if !errors.Is(err, auth.ErrPasskeyFailed) {
			t.Errorf("error = %v, want ErrPasskeyFailed", err)
		}
	})

	t.Run("a malformed or misdirected registration answer fails", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		session := h.authenticate(t, f.patToken)
		for _, answer := range []func(options []byte) []byte{
			func([]byte) []byte { return []byte("{}") },
			func(options []byte) []byte {
				response, _ := softauthn.New("https://evil.example.com").Register(options) // valid options can't fail
				return response
			},
		} {
			options, err := h.service.BeginPasskeyRegistration(t.Context(), session, testSite())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.service.FinishPasskeyRegistration(t.Context(), session, testSite(), "", answer(options), testClient()); !errors.Is(err, auth.ErrPasskeyFailed) {
				t.Errorf("error = %v, want ErrPasskeyFailed", err)
			}
		}
	})

	t.Run("an empty name becomes Passkey", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		session := h.authenticate(t, f.patToken)
		if _, err := registerPasskey(t, h, session, softauthn.New(testSite().Origin), "  "); err != nil {
			t.Fatal(err)
		}
		passkeys, err := h.service.ListPasskeys(t.Context(), session)
		if err != nil || len(passkeys) != 2 || passkeys[1].Name != "Passkey" {
			t.Errorf("passkeys = %+v, %v; want the second named Passkey", passkeys, err)
		}
	})

	t.Run("a passkey request expires after 5 minutes", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		session := f.patPending(t, h)
		options, err := h.service.BeginPasskeyAssertion(t.Context(), session, testSite())
		if err != nil {
			t.Fatal(err)
		}
		response, _ := f.authenticator.Sign(options) // valid options can't fail
		h.clock.Advance(6 * time.Minute)
		// Starting another session's request clears expired ones.
		if _, err := h.service.BeginPasskeyAssertion(t.Context(), f.patPending(t, h), testSite()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), response, testClient()); err == nil {
			t.Error("an expired passkey request was accepted")
		}
	})

	t.Run("a registration request can't be answered as a sign-in", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		session := h.authenticate(t, f.patToken)
		if _, err := h.service.BeginPasskeyRegistration(t.Context(), session, testSite()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), []byte("{}"), testClient()); !errors.Is(err, auth.ErrPasskeyFailed) {
			t.Errorf("error = %v, want ErrPasskeyFailed", err)
		}
	})

	t.Run("listing passkeys needs a signed-in session", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		if _, err := h.service.ListPasskeys(t.Context(), f.patPending(t, h)); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("error = %v, want ErrNotAuthenticated", err)
		}
	})

	t.Run("removing a passkey needs a recent second factor", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		h.clock.Advance(16 * time.Minute)
		if err := h.service.DeletePasskey(t.Context(), h.authenticate(t, f.patToken), 1, testClient()); !errors.Is(err, auth.ErrStepUpRequired) {
			t.Errorf("error = %v, want ErrStepUpRequired", err)
		}
	})

	t.Run("a TOTP-only user can't finish a passkey check", func(t *testing.T) {
		h := newHarness(t)
		_, adminToken, _ := h.setUpAdmin(t)
		_, err := h.service.FinishPasskeyAssertion(t.Context(), h.authenticate(t, adminToken), testSite(), []byte("{}"), testClient())
		if !errors.Is(err, auth.ErrWrongLoginStep) {
			t.Errorf("error = %v, want ErrWrongLoginStep", err)
		}
	})

	t.Run("passkey checks are rate limited", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		session := f.patPending(t, h)
		for range 3 {
			_, _ = h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), []byte("{}"), testClient()) // failures expected
		}
		_, err := h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), []byte("{}"), testClient())
		var tooMany *auth.TooManyAttemptsError
		if !errors.As(err, &tooMany) {
			t.Errorf("error = %v, want backoff", err)
		}
	})

	t.Run("a corrupted stored passkey is reported", func(t *testing.T) {
		h := newHarness(t)
		f := newPasskeyFixture(t, h)
		corruptPasskeys(t, h)
		if _, err := h.service.BeginPasskeyAssertion(t.Context(), h.authenticate(t, f.patToken), testSite()); err == nil || errors.Is(err, auth.ErrPasskeyFailed) {
			t.Errorf("error = %v, want a decoding error", err)
		}
	})
}

func TestPasskeyDatabaseFailuresAreReported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		fault func(t *testing.T, h *harness)
		run   func(t *testing.T, h *harness, f passkeyFixture, inject func()) error
	}{
		{name: "saving a passkey", fault: failing("INSERT", "passkeys"), run: addSecondPasskey},
		{name: "auditing a new passkey", fault: failing("INSERT", "audit_log"), run: addSecondPasskey},
		{name: "creating the user handle", fault: failing("UPDATE", "users"), run: func(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
			inject()
			_, err := h.service.BeginPasskeyRegistration(t.Context(), h.authenticate(t, f.adminToken), testSite())
			return err
		}},
		{name: "reading passkeys for registration", fault: failReadOf("passkeys"), run: func(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
			session := h.authenticate(t, f.patToken)
			inject()
			_, err := h.service.BeginPasskeyRegistration(t.Context(), session, testSite())
			return err
		}},
		{name: "reading passkeys for sign-in", fault: failReadOf("passkeys"), run: func(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
			session := f.patPending(t, h)
			inject()
			_, err := h.service.BeginPasskeyAssertion(t.Context(), session, testSite())
			return err
		}},
		{name: "recording passkey use", fault: failing("UPDATE", "passkeys"), run: passkeySignIn},
		{name: "replacing the session after a passkey", fault: failing("DELETE", "sessions"), run: passkeySignIn},
		{name: "clearing the lock after a passkey", fault: failing("UPDATE", "users"), run: passkeySignIn},
		{name: "auditing a passkey sign-in", fault: failing("INSERT", "audit_log"), run: passkeySignIn},
		{name: "auditing a failed passkey check", fault: failing("INSERT", "audit_log"), run: func(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
			session := f.patPending(t, h)
			inject()
			_, err := h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), []byte("{}"), testClient())
			return expectNot(err, auth.ErrPasskeyFailed)
		}},
		{name: "a passkey step-up", fault: failing("UPDATE", "sessions"), run: func(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
			session := h.authenticate(t, f.patToken)
			options, err := h.service.BeginPasskeyAssertion(t.Context(), session, testSite())
			if err != nil {
				t.Fatal(err)
			}
			response, _ := f.authenticator.Sign(options) // valid options can't fail
			inject()
			_, err = h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), response, testClient())
			return err
		}},
		{name: "listing passkeys", fault: failReadOf("passkeys"), run: func(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
			session := h.authenticate(t, f.patToken)
			inject()
			_, err := h.service.ListPasskeys(t.Context(), session)
			return err
		}},
		{name: "removing a passkey", fault: failing("DELETE", "passkeys"), run: removeAdminPasskey},
		{name: "auditing a removed passkey", fault: failing("INSERT", "audit_log"), run: removeAdminPasskey},
		{name: "recovery codes for a passkey enrollment", fault: failing("INSERT", "recovery_codes"), run: enrollNewcomerWithPasskey},
		{name: "the session for a passkey enrollment", fault: failing("INSERT", "sessions"), run: enrollNewcomerWithPasskey},
	}

	for _, test := range tests {
		t.Run("reports a failure in "+test.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			fixture := newPasskeyFixture(t, h)

			if err := test.run(t, h, fixture, func() { test.fault(t, h) }); err == nil {
				t.Error("returned nil, want the database failure")
			}
		})
	}
}

func addSecondPasskey(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
	session := h.authenticate(t, f.patToken)
	options, err := h.service.BeginPasskeyRegistration(t.Context(), session, testSite())
	if err != nil {
		t.Fatal(err)
	}
	response, _ := softauthn.New(testSite().Origin).Register(options) // valid options can't fail
	inject()
	_, err = h.service.FinishPasskeyRegistration(t.Context(), session, testSite(), "Second", response, testClient())
	return err
}

func passkeySignIn(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
	session := f.patPending(t, h)
	options, err := h.service.BeginPasskeyAssertion(t.Context(), session, testSite())
	if err != nil {
		t.Fatal(err)
	}
	response, _ := f.authenticator.Sign(options) // valid options can't fail
	inject()
	_, err = h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), response, testClient())
	return err
}

func removeAdminPasskey(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
	admin := h.authenticate(t, f.adminToken)
	if _, err := registerPasskey(t, h, admin, softauthn.New(testSite().Origin), "Phone"); err != nil {
		t.Fatal(err)
	}
	passkeys, err := h.service.ListPasskeys(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	inject()
	return h.service.DeletePasskey(t.Context(), admin, passkeys[0].ID, testClient())
}

func enrollNewcomerWithPasskey(t *testing.T, h *harness, f passkeyFixture, inject func()) error {
	if _, err := h.service.CreateUser(t.Context(), h.authenticate(t, f.adminToken), auth.CreateUserInput{Username: "newcomer", Password: strongPassword, Role: auth.RoleViewer}, testClient()); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: "newcomer", Password: strongPassword}, auth.RequestInfo{IP: "192.0.2.91"})
	if err != nil {
		t.Fatal(err)
	}
	pending := h.authenticate(t, result.SessionToken)
	options, err := h.service.BeginPasskeyRegistration(t.Context(), pending, testSite())
	if err != nil {
		t.Fatal(err)
	}
	response, _ := softauthn.New(testSite().Origin).Register(options) // valid options can't fail
	inject()
	_, err = h.service.FinishPasskeyRegistration(t.Context(), pending, testSite(), "", response, testClient())
	return err
}

// corruptPasskeys overwrites every stored passkey with text that isn't JSON.
func corruptPasskeys(t *testing.T, h *harness) {
	t.Helper()
	err := h.database.store.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE passkeys SET credential_json = 'not json'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

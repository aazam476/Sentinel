// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/testutil/softauthn"
)

// testSite is the relying party the passkey tests use.
func testSite() auth.RelyingParty {
	return auth.RelyingParty{ID: "localhost", Origin: "https://localhost:7443"}
}

// enrollWithPasskey creates the operator "pat", who chooses a passkey as
// their first second factor, returning the authenticator and signed-in
// session.
func enrollWithPasskey(t *testing.T, h *harness, adminToken string) (*softauthn.Authenticator, auth.SignedIn) {
	t.Helper()
	const username = "pat"
	if _, err := h.service.CreateUser(t.Context(), h.authenticate(t, adminToken), auth.CreateUserInput{Username: username, Password: strongPassword, Role: auth.RoleOperator}, testClient()); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: username, Password: strongPassword}, testClient())
	if err != nil {
		t.Fatal(err)
	}
	pending := h.authenticate(t, result.SessionToken)
	authenticator := softauthn.New(testSite().Origin)
	signedIn, err := registerPasskey(t, h, pending, authenticator, "Laptop")
	if err != nil {
		t.Fatalf("passkey enrollment failed: %v", err)
	}
	return authenticator, signedIn
}

func registerPasskey(t *testing.T, h *harness, session auth.Session, authenticator *softauthn.Authenticator, name string) (auth.SignedIn, error) {
	t.Helper()
	options, err := h.service.BeginPasskeyRegistration(t.Context(), session, testSite())
	if err != nil {
		return auth.SignedIn{}, err
	}
	response, err := authenticator.Register(options)
	if err != nil {
		t.Fatal(err)
	}
	return h.service.FinishPasskeyRegistration(t.Context(), session, testSite(), name, response, testClient())
}

func assertWithPasskey(t *testing.T, h *harness, session auth.Session, authenticator *softauthn.Authenticator) (auth.SignedIn, error) {
	t.Helper()
	options, err := h.service.BeginPasskeyAssertion(t.Context(), session, testSite())
	if err != nil {
		return auth.SignedIn{}, err
	}
	response, err := authenticator.Sign(options)
	if err != nil {
		t.Fatal(err)
	}
	return h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), response, testClient())
}

func TestSEC04_PasskeysCanReplaceTOTP(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, adminToken, _ := h.setUpAdmin(t)
	authenticator, enrolled := enrollWithPasskey(t, h, adminToken)

	t.Run("a passkey as the first second factor signs in with recovery codes", func(t *testing.T) {
		session := h.authenticate(t, enrolled.SessionToken)
		if !session.IsSignedIn() || !session.HasPasskey || session.HasTOTP || len(enrolled.RecoveryCodes) != 10 {
			t.Errorf("session = %+v with %d codes, want signed in with a passkey and 10 codes", session, len(enrolled.RecoveryCodes))
		}
		if !contains(h.auditActions(t), "auth.passkey.add:success") {
			t.Errorf("audit = %v, want auth.passkey.add", h.auditActions(t))
		}
	})

	t.Run("the passkey finishes a later sign-in", func(t *testing.T) {
		result, err := h.service.Login(t.Context(), auth.LoginInput{Username: "pat", Password: strongPassword}, testClient())
		if err != nil || result.NextStep != auth.NextStepSecondFactor {
			t.Fatalf("Login = %+v, %v; want the second-factor step", result, err)
		}
		signedIn, err := assertWithPasskey(t, h, h.authenticate(t, result.SessionToken), authenticator)
		if err != nil {
			t.Fatalf("passkey sign-in returned %v", err)
		}
		if !h.authenticate(t, signedIn.SessionToken).IsSignedIn() {
			t.Error("the new session is not signed in")
		}
		if _, err := h.service.Authenticate(t.Context(), result.SessionToken); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("the password-only token still works: %v", err)
		}
	})

	t.Run("the passkey works for step-up", func(t *testing.T) {
		h.clock.Advance(16 * time.Minute)
		session := h.authenticate(t, enrolled.SessionToken)
		if err := h.service.RequireRecentSecondFactor(session); !errors.Is(err, auth.ErrStepUpRequired) {
			t.Fatalf("RequireRecentSecondFactor = %v, want step-up required", err)
		}
		if _, err := assertWithPasskey(t, h, session, authenticator); err != nil {
			t.Fatalf("passkey step-up returned %v", err)
		}
		if err := h.service.RequireRecentSecondFactor(h.authenticate(t, enrolled.SessionToken)); err != nil {
			t.Errorf("after step-up: %v", err)
		}
	})
}

func TestPasskeyChecksRefuseForgeries(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, adminToken, _ := h.setUpAdmin(t)
	authenticator, _ := enrollWithPasskey(t, h, adminToken)
	pending := func(t *testing.T) auth.Session {
		h.clock.Advance(time.Minute) // stay clear of rate limiting between cases
		result, err := h.service.Login(t.Context(), auth.LoginInput{Username: "pat", Password: strongPassword}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		return h.authenticate(t, result.SessionToken)
	}

	t.Run("refuses an answer signed for another site", func(t *testing.T) {
		session := pending(t)
		options, err := h.service.BeginPasskeyAssertion(t.Context(), session, testSite())
		if err != nil {
			t.Fatal(err)
		}
		phishing := softauthn.New("https://evil.example.com")
		*phishing = *authenticator
		phishing.Origin = "https://evil.example.com"
		response, err := phishing.Sign(options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), response, testClient()); !errors.Is(err, auth.ErrPasskeyFailed) {
			t.Errorf("error = %v, want ErrPasskeyFailed", err)
		}
	})

	t.Run("refuses a passkey the user never registered", func(t *testing.T) {
		session := pending(t)
		if _, err := assertWithPasskey(t, h, session, softauthn.New(testSite().Origin)); !errors.Is(err, auth.ErrPasskeyFailed) {
			t.Errorf("error = %v, want ErrPasskeyFailed", err)
		}
	})

	t.Run("refuses an answer replayed after it was used", func(t *testing.T) {
		session := pending(t)
		options, err := h.service.BeginPasskeyAssertion(t.Context(), session, testSite())
		if err != nil {
			t.Fatal(err)
		}
		response, _ := authenticator.Sign(options) // checked by the tests above
		if _, err := h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), response, testClient()); err != nil {
			t.Fatal(err)
		}
		again := pending(t)
		// A fresh challenge is pending, but the replayed answer signs the old one.
		if _, err := h.service.BeginPasskeyAssertion(t.Context(), again, testSite()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.FinishPasskeyAssertion(t.Context(), again, testSite(), response, testClient()); !errors.Is(err, auth.ErrPasskeyFailed) {
			t.Errorf("replay error = %v, want ErrPasskeyFailed", err)
		}
	})

	t.Run("refuses malformed answers", func(t *testing.T) {
		session := pending(t)
		if _, err := h.service.BeginPasskeyAssertion(t.Context(), session, testSite()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.FinishPasskeyAssertion(t.Context(), session, testSite(), []byte("{}"), testClient()); !errors.Is(err, auth.ErrPasskeyFailed) {
			t.Errorf("error = %v, want ErrPasskeyFailed", err)
		}
	})

	t.Run("audits passkey failures", func(t *testing.T) {
		if countOf(h.auditActions(t), "auth.login:failure") < 3 {
			t.Errorf("audit = %v, want the failed passkey checks", h.auditActions(t))
		}
	})
}

func TestPasskeyManagement(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	admin, adminToken, _ := h.setUpAdmin(t)
	_, enrolled := enrollWithPasskey(t, h, adminToken)
	pat := h.authenticate(t, enrolled.SessionToken)

	t.Run("lists the user's passkeys", func(t *testing.T) {
		passkeys, err := h.service.ListPasskeys(t.Context(), pat)
		if err != nil || len(passkeys) != 1 || passkeys[0].Name != "Laptop" {
			t.Errorf("ListPasskeys = %+v, %v; want the Laptop passkey", passkeys, err)
		}
	})

	t.Run("refuses to remove the only second factor", func(t *testing.T) {
		passkeys, _ := h.service.ListPasskeys(t.Context(), pat) // checked above
		if err := h.service.DeletePasskey(t.Context(), pat, passkeys[0].ID, testClient()); !errors.Is(err, auth.ErrLastSecondFactor) {
			t.Errorf("error = %v, want ErrLastSecondFactor", err)
		}
	})

	t.Run("an admin with TOTP adds and removes a passkey", func(t *testing.T) {
		adminSession := h.authenticate(t, adminToken)
		if _, err := registerPasskey(t, h, adminSession, softauthn.New(testSite().Origin), "Phone"); err != nil {
			t.Fatalf("registerPasskey returned %v", err)
		}
		passkeys, err := h.service.ListPasskeys(t.Context(), adminSession)
		if err != nil || len(passkeys) != 1 {
			t.Fatalf("ListPasskeys = %+v, %v", passkeys, err)
		}
		if err := h.service.DeletePasskey(t.Context(), adminSession, passkeys[0].ID, testClient()); err != nil {
			t.Errorf("DeletePasskey returned %v", err)
		}
		if err := h.service.DeletePasskey(t.Context(), adminSession, passkeys[0].ID, testClient()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("deleting again: %v, want ErrNotFound", err)
		}
	})

	t.Run("adding a passkey needs a recent second factor", func(t *testing.T) {
		h.clock.Advance(16 * time.Minute)
		_, err := registerPasskey(t, h, h.authenticate(t, adminToken), softauthn.New(testSite().Origin), "Late")
		if !errors.Is(err, auth.ErrStepUpRequired) {
			t.Errorf("error = %v, want ErrStepUpRequired", err)
		}
	})

	t.Run("a TOTP user without passkeys can't start a passkey check", func(t *testing.T) {
		if _, err := h.service.BeginPasskeyAssertion(t.Context(), h.authenticate(t, adminToken), testSite()); !errors.Is(err, auth.ErrWrongLoginStep) {
			t.Errorf("error = %v, want ErrWrongLoginStep", err)
		}
	})

	t.Run("refuses a name over 64 characters", func(t *testing.T) {
		h.clock.Advance(30 * time.Second)
		if err := h.service.StepUp(t.Context(), h.authenticate(t, adminToken), h.code(admin.totpSecret), testClient()); err != nil {
			t.Fatal(err)
		}
		long := strings.Repeat("k", 65)
		var validation *auth.ValidationError
		if _, err := registerPasskey(t, h, h.authenticate(t, adminToken), softauthn.New(testSite().Origin), long); !errors.As(err, &validation) {
			t.Errorf("error = %v, want a validation error", err)
		}
	})

	t.Run("refuses a name with control or bidirectional characters", func(t *testing.T) {
		for _, name := range []string{"YubiKey\x00", "Work \u202eyeK"} {
			var validation *auth.ValidationError
			if _, err := registerPasskey(t, h, h.authenticate(t, adminToken), softauthn.New(testSite().Origin), name); !errors.As(err, &validation) {
				t.Errorf("%q: error = %v, want a validation error", name, err)
			}
		}
	})

	t.Run("reset-admin removes passkeys", func(t *testing.T) {
		if _, err := registerPasskey(t, h, h.authenticate(t, adminToken), softauthn.New(testSite().Origin), "Phone"); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.ResetAdmin(t.Context(), "admin"); err != nil {
			t.Fatal(err)
		}
		if got := h.queryString(t, `SELECT CAST(COUNT(*) AS TEXT) FROM passkeys p JOIN users u ON u.id = p.user_id WHERE u.username = 'admin'`); got != "0" {
			t.Errorf("admin passkeys after reset = %s, want 0", got)
		}
	})
}

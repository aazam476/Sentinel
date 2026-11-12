// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// setUpAdminWithout2FA completes setup skipping 2FA and returns the admin's
// session token.
func (h *harness) setUpAdminWithout2FA(t *testing.T) (string, error) {
	t.Helper()
	begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{Username: "admin", Password: strongPassword}, testClient())
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupID: begun.SetupID, SkipTwoFactor: true}, testClient())
	if err != nil {
		return "", err
	}
	if len(signedIn.RecoveryCodes) != 0 {
		t.Errorf("recovery codes without 2FA: %v", signedIn.RecoveryCodes)
	}
	return signedIn.SessionToken, nil
}

func (h *harness) loginStep(t *testing.T, username string) (auth.LoginResult, error) {
	t.Helper()
	return h.service.Login(t.Context(), auth.LoginInput{Username: username, Password: strongPassword}, testClient())
}

func TestSEC04_TwoFactorIsEachUsersChoiceByDefault(t *testing.T) {
	h := newOptionalHarness(t)
	if required, err := h.service.TwoFactorRequired(t.Context()); err != nil || required {
		t.Fatalf("TwoFactorRequired on a fresh server = %v, %v; want false", required, err)
	}
	adminToken, err := h.setUpAdminWithout2FA(t)
	if err != nil {
		t.Fatal(err)
	}
	admin := h.authenticate(t, adminToken)
	if !admin.IsSignedIn() || admin.HasSecondFactor {
		t.Fatalf("admin = %+v, want signed in without 2FA", admin)
	}
	if _, err := h.service.CreateUser(t.Context(), admin, auth.CreateUserInput{Username: "ann", Password: strongPassword, Role: auth.RoleOperator}, testClient()); err != nil {
		t.Fatal(err)
	}

	t.Run("a password alone signs in a user without 2FA", func(t *testing.T) {
		result, err := h.loginStep(t, "ann")
		if err != nil || result.NextStep != auth.NextStepNone {
			t.Fatalf("Login = %+v, %v; want signed in", result, err)
		}
		if session := h.authenticate(t, result.SessionToken); !session.IsSignedIn() {
			t.Error("the session isn't signed in")
		}
	})

	t.Run("SEC-07 step-up asks for the password again", func(t *testing.T) {
		result, _ := h.loginStep(t, "ann")
		h.clock.Advance(auth.StepUpWindow + time.Minute)
		session := h.authenticate(t, result.SessionToken)
		if err := h.service.RequireRecentSecondFactor(session); !errors.Is(err, auth.ErrStepUpRequired) {
			t.Fatalf("RequireRecentSecondFactor = %v", err)
		}
		if err := h.service.StepUpWithPassword(t.Context(), session, "wrong password", testClient()); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Errorf("wrong password: %v", err)
		}
		if err := h.service.StepUpWithPassword(t.Context(), session, strongPassword, testClient()); err != nil {
			t.Fatal(err)
		}
		if err := h.service.RequireRecentSecondFactor(h.authenticate(t, result.SessionToken)); err != nil {
			t.Errorf("after the password: %v", err)
		}
	})

	t.Run("adding an authenticator app from the account makes it ask for codes", func(t *testing.T) {
		result, _ := h.loginStep(t, "ann")
		session := h.authenticate(t, result.SessionToken)
		enrollment, err := h.service.BeginTOTPEnrollment(t.Context(), session)
		if err != nil {
			t.Fatal(err)
		}
		signedIn, err := h.service.ConfirmTOTPEnrollment(t.Context(), session, h.code(enrollment.Secret), testClient())
		if err != nil || len(signedIn.RecoveryCodes) != 10 {
			t.Fatalf("ConfirmTOTPEnrollment = %d codes, %v", len(signedIn.RecoveryCodes), err)
		}
		h.clock.Advance(30 * time.Second)
		if next, _ := h.loginStep(t, "ann"); next.NextStep != auth.NextStepSecondFactor {
			t.Errorf("next login = %q, want the second factor", next.NextStep)
		}
		withTOTP := h.authenticate(t, signedIn.SessionToken)
		if _, err := h.service.BeginTOTPEnrollment(t.Context(), withTOTP); !errors.Is(err, auth.ErrWrongLoginStep) {
			t.Errorf("a second authenticator app: %v", err)
		}
		if err := h.service.StepUpWithPassword(t.Context(), withTOTP, strongPassword, testClient()); !errors.Is(err, auth.ErrWrongLoginStep) {
			t.Errorf("a password step-up with 2FA set up: %v", err)
		}

		if err := h.service.RemoveTOTP(t.Context(), withTOTP, testClient()); err != nil {
			t.Fatal(err)
		}
		if next, _ := h.loginStep(t, "ann"); next.NextStep != auth.NextStepNone {
			t.Errorf("after removing it, login = %q, want signed in", next.NextStep)
		}
		if got := h.queryString(t, "SELECT COUNT(*) FROM recovery_codes"); got != "0" {
			t.Errorf("%s recovery codes left", got)
		}
		if err := h.service.RemoveTOTP(t.Context(), h.authenticate(t, signedIn.SessionToken), testClient()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("removing it twice: %v", err)
		}
	})
}

func TestSEC04_AnAdminCanRequireTwoFactor(t *testing.T) {
	h := newOptionalHarness(t)
	adminAccount, adminToken, _ := h.setUpAdmin(t)
	admin := h.authenticate(t, adminToken)
	if _, err := h.service.CreateUser(t.Context(), admin, auth.CreateUserInput{Username: "ann", Password: strongPassword, Role: auth.RoleViewer}, testClient()); err != nil {
		t.Fatal(err)
	}
	ann, _ := h.loginStep(t, "ann")

	t.Run("only an admin who uses 2FA can turn it on", func(t *testing.T) {
		if err := h.service.SetTwoFactorRequired(t.Context(), h.authenticate(t, ann.SessionToken), true, testClient()); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("a viewer: %v", err)
		}
		other := newOptionalHarness(t)
		token, err := other.setUpAdminWithout2FA(t)
		if err != nil {
			t.Fatal(err)
		}
		if err := other.service.SetTwoFactorRequired(t.Context(), other.authenticate(t, token), true, testClient()); !errors.Is(err, auth.ErrOwnSecondFactorNeeded) {
			t.Errorf("an admin without 2FA: %v", err)
		}
	})

	t.Run("turning it on signs out users without 2FA and makes them set it up", func(t *testing.T) {
		if err := h.service.SetTwoFactorRequired(t.Context(), admin, true, testClient()); err != nil {
			t.Fatal(err)
		}
		if required, _ := h.service.TwoFactorRequired(t.Context()); !required {
			t.Error("not required after turning it on")
		}
		if _, err := h.service.Authenticate(t.Context(), ann.SessionToken); err == nil {
			t.Error("a user without 2FA is still signed in")
		}
		h.authenticate(t, adminToken) // the admin, who has 2FA, stays
		if next, _ := h.loginStep(t, "ann"); next.NextStep != auth.NextStepEnrollSecondFactor {
			t.Errorf("ann's next login = %q, want enrollment", next.NextStep)
		}
	})

	t.Run("with it on, the last second factor can't be removed", func(t *testing.T) {
		if err := h.service.RemoveTOTP(t.Context(), admin, testClient()); !errors.Is(err, auth.ErrLastSecondFactor) {
			t.Errorf("RemoveTOTP = %v", err)
		}
	})

	t.Run("turning it off again is recorded", func(t *testing.T) {
		session := h.authenticate(t, h.signIn(t, adminAccount))
		if err := h.service.SetTwoFactorRequired(t.Context(), session, false, testClient()); err != nil {
			t.Fatal(err)
		}
		if required, _ := h.service.TwoFactorRequired(t.Context()); required {
			t.Error("still required")
		}
		actions := h.auditActions(t)
		count := 0
		for _, action := range actions {
			if action == "settings.require_two_factor:success" {
				count++
			}
		}
		if count != 2 {
			t.Errorf("audit actions = %v, want the setting changed twice", actions)
		}
	})
}

func TestSEC04_SetupCantSkip2FAWhereItIsRequired(t *testing.T) {
	h := newHarness(t) // required, as on a server restored from a backup of one
	if _, err := h.setUpAdminWithout2FA(t); !errors.Is(err, auth.ErrTwoFactorRequired) {
		t.Errorf("CompleteSetup skipping 2FA = %v, want ErrTwoFactorRequired", err)
	}
}

func TestSEC04_WithoutTheRequirementTheLastPasskeyCanGo(t *testing.T) {
	h := newHarness(t) // pat enrolls a passkey the way the required mode makes them
	_, adminToken, _ := h.setUpAdmin(t)
	_, enrolled := enrollWithPasskey(t, h, adminToken)
	h.setTwoFactorRequired(t, false)
	pat := h.authenticate(t, enrolled.SessionToken)
	passkeys, err := h.service.ListPasskeys(t.Context(), pat)
	if err != nil || len(passkeys) != 1 {
		t.Fatalf("ListPasskeys = %+v, %v", passkeys, err)
	}
	if err := h.service.DeletePasskey(t.Context(), pat, passkeys[0].ID, testClient()); err != nil {
		t.Fatalf("DeletePasskey = %v, want the last passkey removed", err)
	}
	if got := h.queryString(t, "SELECT COUNT(*) FROM recovery_codes WHERE user_id = (SELECT id FROM users WHERE username = 'pat')"); got != "0" {
		t.Errorf("%s recovery codes left without a second factor", got)
	}
	if next, _ := h.loginStep(t, "pat"); next.NextStep != auth.NextStepNone {
		t.Errorf("next login = %q, want signed in", next.NextStep)
	}
}

func TestSEC05_PasswordStepUpIsRateLimited(t *testing.T) {
	h := newOptionalHarness(t)
	f := newOptionalFixture(t, h)
	var limited *auth.TooManyAttemptsError
	for range 12 {
		err := h.service.StepUpWithPassword(t.Context(), f.ann, "wrong password", testClient())
		if errors.As(err, &limited) {
			return
		}
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("err = %v", err)
		}
	}
	t.Error("12 wrong passwords in a row were never slowed down")
}

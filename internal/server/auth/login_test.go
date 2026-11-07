// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

func TestSEC03_PasswordsAreHashedWithArgon2id(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setUpAdmin(t)

	stored := h.queryString(t, `SELECT password_hash FROM users WHERE username = 'admin'`)

	if !strings.HasPrefix(stored, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Errorf("stored password hash = %q, want Argon2id with 64 MiB, 3 iterations, parallelism 2", stored)
	}
	if strings.Contains(stored, strongPassword) {
		t.Error("stored password hash contains the password")
	}
}

func TestSEC04_TwoFactorIsRequiredToSignIn(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	admin, adminToken, recoveryCodes := h.setUpAdmin(t)

	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: admin.password}, testClient())
	if err != nil {
		t.Fatalf("Login returned %v", err)
	}
	pending := h.authenticate(t, result.SessionToken)

	t.Run("a correct password only reaches the second-factor step", func(t *testing.T) {
		if result.NextStep != auth.NextStepSecondFactor || pending.IsSignedIn() {
			t.Errorf("next step = %q, signed in = %v; want second_factor and not signed in", result.NextStep, pending.IsSignedIn())
		}
	})

	t.Run("a password-only session can't do anything else", func(t *testing.T) {
		if _, err := h.service.ListUsers(t.Context(), pending); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("ListUsers error = %v, want ErrNotAuthenticated", err)
		}
	})

	t.Run("a wrong TOTP code is refused and audited", func(t *testing.T) {
		_, err := h.service.VerifySecondFactor(t.Context(), pending, auth.SecondFactorInput{TOTPCode: "000000"}, testClient())
		if !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("VerifySecondFactor error = %v, want ErrInvalidCode", err)
		}
		if !contains(h.auditActions(t), "auth.login:failure") {
			t.Errorf("audit = %v, want a failed login", h.auditActions(t))
		}
	})

	var signedInToken string
	t.Run("a correct TOTP code signs in with a new session token", func(t *testing.T) {
		code := h.code(admin.totpSecret)
		signedIn, err := h.service.VerifySecondFactor(t.Context(), pending, auth.SecondFactorInput{TOTPCode: code}, testClient())
		if err != nil {
			t.Fatalf("VerifySecondFactor returned %v", err)
		}
		signedInToken = signedIn.SessionToken
		if signedIn.SessionToken == result.SessionToken {
			t.Error("the session token was not replaced at sign-in")
		}
		if session := h.authenticate(t, signedIn.SessionToken); !session.IsSignedIn() {
			t.Error("the new session is not signed in")
		}
		if _, err := h.service.Authenticate(t.Context(), result.SessionToken); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("old token still works: %v", err)
		}
	})

	t.Run("the same TOTP code can't be used twice", func(t *testing.T) {
		code := h.code(admin.totpSecret)
		again, err := h.service.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: admin.password}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.service.VerifySecondFactor(t.Context(), h.authenticate(t, again.SessionToken), auth.SecondFactorInput{TOTPCode: code}, testClient())
		if !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("replayed code error = %v, want ErrInvalidCode", err)
		}
	})

	t.Run("a recovery code works once", func(t *testing.T) {
		first := loginWithRecoveryCode(t, h, admin, recoveryCodes[0])
		if first.err != nil || first.remaining != 9 || !first.used {
			t.Errorf("first use = %v with %d left, want success with 9 left", first.err, first.remaining)
		}
		second := loginWithRecoveryCode(t, h, admin, recoveryCodes[0])
		if !errors.Is(second.err, auth.ErrInvalidCode) {
			t.Errorf("second use error = %v, want ErrInvalidCode", second.err)
		}
	})

	t.Run("a recovery code is accepted in lower case without dashes", func(t *testing.T) {
		// Three failures above put the username into backoff; wait it out.
		h.clock.Advance(time.Minute)
		relaxed := strings.ToLower(strings.ReplaceAll(recoveryCodes[1], "-", " "))
		if result := loginWithRecoveryCode(t, h, admin, relaxed); result.err != nil {
			t.Errorf("relaxed recovery code returned %v", result.err)
		}
	})

	t.Run("a signed-in session can't redo the second-factor step", func(t *testing.T) {
		signedIn := h.authenticate(t, signedInToken)
		if _, err := h.service.VerifySecondFactor(t.Context(), signedIn, auth.SecondFactorInput{TOTPCode: "123456"}, testClient()); !errors.Is(err, auth.ErrWrongLoginStep) {
			t.Errorf("error = %v, want ErrWrongLoginStep", err)
		}
	})

	_ = adminToken // the setup session is covered by the setup tests
}

func TestSEC04_UsersWithout2FAMustEnrollFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, adminToken, _ := h.setUpAdmin(t)
	admin := h.authenticate(t, adminToken)
	if _, err := h.service.CreateUser(t.Context(), admin, auth.CreateUserInput{Username: "bob", Password: strongPassword, Role: auth.RoleOperator}, testClient()); err != nil {
		t.Fatal(err)
	}

	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: "bob", Password: strongPassword}, testClient())
	if err != nil {
		t.Fatalf("Login returned %v", err)
	}
	pending := h.authenticate(t, result.SessionToken)

	t.Run("is sent to enrollment after the password", func(t *testing.T) {
		if result.NextStep != auth.NextStepEnrollSecondFactor || pending.HasSecondFactor {
			t.Errorf("next step = %q, want enroll_second_factor", result.NextStep)
		}
	})

	t.Run("can't skip enrollment by sending a code", func(t *testing.T) {
		if _, err := h.service.VerifySecondFactor(t.Context(), pending, auth.SecondFactorInput{TOTPCode: "123456"}, testClient()); !errors.Is(err, auth.ErrWrongLoginStep) {
			t.Errorf("error = %v, want ErrWrongLoginStep", err)
		}
	})

	t.Run("a code can't confirm enrollment before it has started", func(t *testing.T) {
		if _, err := h.service.ConfirmTOTPEnrollment(t.Context(), pending, "123456", testClient()); !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("error = %v, want ErrInvalidCode", err)
		}
	})

	enrollment, err := h.service.BeginTOTPEnrollment(t.Context(), pending)
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment returned %v", err)
	}

	t.Run("a wrong confirmation code is refused", func(t *testing.T) {
		if _, err := h.service.ConfirmTOTPEnrollment(t.Context(), pending, "000000", testClient()); !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("error = %v, want ErrInvalidCode", err)
		}
	})

	t.Run("a correct code finishes enrollment and signs in", func(t *testing.T) {
		signedIn, err := h.service.ConfirmTOTPEnrollment(t.Context(), pending, h.code(enrollment.Secret), testClient())
		if err != nil {
			t.Fatalf("ConfirmTOTPEnrollment returned %v", err)
		}
		session := h.authenticate(t, signedIn.SessionToken)
		if !session.IsSignedIn() || !session.HasSecondFactor || len(signedIn.RecoveryCodes) != 10 {
			t.Errorf("session = %+v with %d recovery codes, want signed in with 2FA and 10 codes", session, len(signedIn.RecoveryCodes))
		}
		if !contains(h.auditActions(t), "auth.totp.enroll:success") {
			t.Errorf("audit = %v, want auth.totp.enroll", h.auditActions(t))
		}
	})

	t.Run("enrollment can't be restarted once done", func(t *testing.T) {
		h.clock.Advance(30 * time.Second)
		again, err := h.service.Login(t.Context(), auth.LoginInput{Username: "bob", Password: strongPassword}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		if again.NextStep != auth.NextStepSecondFactor {
			t.Errorf("next step = %q, want second_factor", again.NextStep)
		}
		if _, err := h.service.BeginTOTPEnrollment(t.Context(), h.authenticate(t, again.SessionToken)); !errors.Is(err, auth.ErrWrongLoginStep) {
			t.Errorf("BeginTOTPEnrollment error = %v, want ErrWrongLoginStep", err)
		}
	})
}

func TestLoginRefusesWrongCredentials(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	admin, _, _ := h.setUpAdmin(t)

	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "refuses a wrong password", username: admin.username, password: "not the right password"},
		{name: "refuses an unknown username with the same error", username: "nobody", password: strongPassword},
		{name: "refuses an empty password", username: admin.username, password: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := h.service.Login(t.Context(), auth.LoginInput{Username: test.username, Password: test.password}, auth.RequestInfo{IP: "198.51.100." + test.name[:1]})
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Errorf("Login error = %v, want ErrInvalidCredentials", err)
			}
		})
	}

	t.Run("matches the username in any letter case", func(t *testing.T) {
		_, err := h.service.Login(t.Context(), auth.LoginInput{Username: "ADMIN", Password: admin.password}, auth.RequestInfo{IP: "192.0.2.1"})
		if err != nil {
			t.Errorf("Login with uppercase username returned %v", err)
		}
	})
}

// recoveryLogin is the outcome of a login finished with a recovery code.
type recoveryLogin struct {
	err       error
	used      bool
	remaining int
}

func loginWithRecoveryCode(t *testing.T, h *harness, user account, code string) recoveryLogin {
	t.Helper()
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: user.username, Password: user.password}, testClient())
	if err != nil {
		t.Fatal(err)
	}
	signedIn, err := h.service.VerifySecondFactor(t.Context(), h.authenticate(t, result.SessionToken), auth.SecondFactorInput{RecoveryCode: code}, testClient())
	return recoveryLogin{err: err, used: signedIn.UsedRecoveryCode, remaining: signedIn.RemainingRecoveryCodes}
}

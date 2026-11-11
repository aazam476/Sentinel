// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

func TestSetupIsOpenOnlyUntilAnAdminExists(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	t.Run("lets whoever comes first begin setup, with no token", func(t *testing.T) {
		required, err := h.service.SetupRequired(t.Context())
		if err != nil || !required {
			t.Fatalf("SetupRequired = %v, %v; want true", required, err)
		}
		if _, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{Username: "admin", Password: strongPassword}, testClient()); err != nil {
			t.Errorf("BeginSetup returned %v", err)
		}
	})

	h.setUpAdmin(t)

	t.Run("reports that no setup is needed", func(t *testing.T) {
		required, err := h.service.SetupRequired(t.Context())
		if err != nil || required {
			t.Errorf("SetupRequired = %v, %v; want false", required, err)
		}
	})

	t.Run("refuses to complete a second setup", func(t *testing.T) {
		_, err := h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupID: "x", TOTPCode: "000000"}, testClient())
		if !errors.Is(err, auth.ErrSetupNotAvailable) {
			t.Errorf("CompleteSetup error = %v, want ErrSetupNotAvailable", err)
		}
	})

	t.Run("refuses a second setup", func(t *testing.T) {
		_, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{Username: "x", Password: strongPassword}, testClient())
		if !errors.Is(err, auth.ErrSetupNotAvailable) {
			t.Errorf("BeginSetup error = %v, want ErrSetupNotAvailable", err)
		}
	})

	t.Run("removes a setup-token file an older server left", func(t *testing.T) {
		path := filepath.Join(h.dataDir, "setup-token")
		if err := os.WriteFile(path, []byte("LEFTOVER\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := h.service.RemoveLeftoverSetupToken(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("leftover setup-token still exists: %v", err)
		}
		if err := h.service.RemoveLeftoverSetupToken(); err != nil {
			t.Errorf("removing it again = %v, want nothing to do", err)
		}
	})
}

func TestSEC02_SetupCreatesAnAdminWithTOTPAndRecoveryCodes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	admin, sessionToken, recoveryCodes := h.setUpAdmin(t)

	t.Run("signs the new admin in", func(t *testing.T) {
		session := h.authenticate(t, sessionToken)
		if !session.IsSignedIn() || session.Role != auth.RoleAdmin || session.Username != admin.username || !session.HasSecondFactor {
			t.Errorf("session = %+v, want a signed-in admin with 2FA", session)
		}
	})

	t.Run("issues ten different recovery codes", func(t *testing.T) {
		seen := map[string]bool{}
		for _, code := range recoveryCodes {
			seen[code] = true
		}
		if len(recoveryCodes) != 10 || len(seen) != 10 {
			t.Errorf("recovery codes = %v, want 10 distinct codes", recoveryCodes)
		}
	})

	t.Run("records the setup in the audit log", func(t *testing.T) {
		if !contains(h.auditActions(t), "setup.complete:success") {
			t.Errorf("audit = %v, want setup.complete", h.auditActions(t))
		}
	})
}

func TestSEC02_SetupChecksThePasswordAndTOTP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		password string
		wantErr  string
	}{
		{name: "refuses a password under 12 characters", username: "admin", password: "short-pass", wantErr: "at least 12"},
		{name: "refuses a commonly used password", username: "admin", password: "1qaz2wsx3edc", wantErr: "commonly used"},
		{name: "refuses a common password in other letter case", username: "admin", password: "1QAZ2WSX3EDC", wantErr: "commonly used"},
		{name: "refuses the username as the password", username: "administrator", password: "Administrator", wantErr: "same as the username"},
		{name: "refuses a password over 256 characters", username: "admin", password: string(make([]byte, 257)), wantErr: "at most 256"},
		{name: "refuses an invalid username", username: "admin user!", password: strongPassword, wantErr: "letters, digits"},
		{name: "refuses an empty username", username: "", password: strongPassword, wantErr: "letters, digits"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)

			_, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{Username: test.username, Password: test.password}, testClient())

			var validation *auth.ValidationError
			if !errors.As(err, &validation) || !strings.Contains(validation.Message, test.wantErr) {
				t.Errorf("BeginSetup error = %v, want a validation error mentioning %q", err, test.wantErr)
			}
		})
	}

	t.Run("creates nothing until a correct TOTP code is entered", func(t *testing.T) {
		h := newHarness(t)
		begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{Username: "admin", Password: strongPassword}, testClient())
		if err != nil {
			t.Fatal(err)
		}

		_, err = h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupID: begun.SetupID, TOTPCode: "000000"}, testClient())

		if !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("CompleteSetup error = %v, want ErrInvalidCode", err)
		}
		if required, _ := h.service.SetupRequired(t.Context()); !required { // an error would also leave this false
			t.Error("a user was created before the TOTP code was confirmed")
		}
	})

	t.Run("expires a setup left unfinished for 15 minutes", func(t *testing.T) {
		h := newHarness(t)
		begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{Username: "admin", Password: strongPassword}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		h.clock.Advance(16 * time.Minute)

		_, err = h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupID: begun.SetupID, TOTPCode: h.code(begun.TOTP.Secret)}, testClient())

		if !errors.Is(err, auth.ErrSetupExpired) {
			t.Errorf("CompleteSetup error = %v, want ErrSetupExpired", err)
		}
	})

	t.Run("gives an authenticator QR code and otpauth URL", func(t *testing.T) {
		h := newHarness(t)
		begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{Username: "admin", Password: strongPassword}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(begun.TOTP.URL, "otpauth://totp/Sentinel:admin") || !strings.Contains(begun.TOTP.QRCode, "data:image/png;base64,") {
			t.Errorf("enrollment = %+v, want an otpauth URL and a PNG data URL", begun.TOTP)
		}
	})
}

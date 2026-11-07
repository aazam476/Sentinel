// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

func TestSEC01_SetupTokenGuardsFirstBoot(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	token, err := h.service.PrepareSetupToken(t.Context())
	if err != nil {
		t.Fatalf("PrepareSetupToken returned %v", err)
	}

	t.Run("creates a 256-bit base32 token", func(t *testing.T) {
		if len(token) != 52 {
			t.Errorf("token %q has %d characters, want 52 (32 bytes of base32)", token, len(token))
		}
	})

	t.Run("writes it to setup-token readable only by the server", func(t *testing.T) {
		info, err := os.Stat(filepath.Join(h.dataDir, "setup-token"))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("setup-token mode = %v, want 0600", info.Mode().Perm())
		}
	})

	t.Run("keeps the same token across restarts until setup is done", func(t *testing.T) {
		again, err := h.restart(t).PrepareSetupToken(t.Context())
		if err != nil || again != token {
			t.Errorf("second PrepareSetupToken = %q, %v; want the same token", again, err)
		}
	})

	t.Run("refuses setup with a wrong token and audits it", func(t *testing.T) {
		_, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: "WRONG", Username: "admin", Password: strongPassword}, testClient())
		if !errors.Is(err, auth.ErrInvalidSetupToken) {
			t.Errorf("BeginSetup error = %v, want ErrInvalidSetupToken", err)
		}
		if !contains(h.auditActions(t), "setup:failure") {
			t.Errorf("audit = %v, want a setup failure", h.auditActions(t))
		}
	})

	t.Run("accepts the token in lower case, as people may type it", func(t *testing.T) {
		_, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: strings.ToLower(token), Username: "admin", Password: strongPassword}, testClient())
		if err != nil {
			t.Errorf("BeginSetup with lowercase token returned %v", err)
		}
	})
}

func TestSEC01_SetupTokenIsDeletedAfterSetup(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setUpAdmin(t)

	t.Run("removes the token file", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(h.dataDir, "setup-token")); !os.IsNotExist(err) {
			t.Errorf("setup-token still exists: %v", err)
		}
	})

	t.Run("reports that no setup is needed", func(t *testing.T) {
		required, err := h.service.SetupRequired(t.Context())
		if err != nil || required {
			t.Errorf("SetupRequired = %v, %v; want false", required, err)
		}
		token, err := h.service.PrepareSetupToken(t.Context())
		if err != nil || token != "" {
			t.Errorf("PrepareSetupToken = %q, %v; want no token", token, err)
		}
	})

	t.Run("refuses to complete a second setup", func(t *testing.T) {
		_, err := h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupToken: "anything", SetupID: "x", TOTPCode: "000000"}, testClient())
		if !errors.Is(err, auth.ErrSetupNotAvailable) {
			t.Errorf("CompleteSetup error = %v, want ErrSetupNotAvailable", err)
		}
	})

	t.Run("refuses a second setup", func(t *testing.T) {
		_, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: "anything", Username: "x", Password: strongPassword}, testClient())
		if !errors.Is(err, auth.ErrSetupNotAvailable) {
			t.Errorf("BeginSetup error = %v, want ErrSetupNotAvailable", err)
		}
	})

	t.Run("removes a leftover token file at the next start", func(t *testing.T) {
		path := filepath.Join(h.dataDir, "setup-token")
		if err := os.WriteFile(path, []byte("LEFTOVER\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.PrepareSetupToken(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("leftover setup-token still exists: %v", err)
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
			token, err := h.service.PrepareSetupToken(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			_, err = h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: token, Username: test.username, Password: test.password}, testClient())

			var validation *auth.ValidationError
			if !errors.As(err, &validation) || !strings.Contains(validation.Message, test.wantErr) {
				t.Errorf("BeginSetup error = %v, want a validation error mentioning %q", err, test.wantErr)
			}
		})
	}

	t.Run("creates nothing until a correct TOTP code is entered", func(t *testing.T) {
		h := newHarness(t)
		token, _ := h.service.PrepareSetupToken(t.Context()) // checked by the tests above
		begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: token, Username: "admin", Password: strongPassword}, testClient())
		if err != nil {
			t.Fatal(err)
		}

		_, err = h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupToken: token, SetupID: begun.SetupID, TOTPCode: "000000"}, testClient())

		if !errors.Is(err, auth.ErrInvalidCode) {
			t.Errorf("CompleteSetup error = %v, want ErrInvalidCode", err)
		}
		if required, _ := h.service.SetupRequired(t.Context()); !required { // an error would also leave this false
			t.Error("a user was created before the TOTP code was confirmed")
		}
	})

	t.Run("expires a setup left unfinished for 15 minutes", func(t *testing.T) {
		h := newHarness(t)
		token, _ := h.service.PrepareSetupToken(t.Context()) // checked by the tests above
		begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: token, Username: "admin", Password: strongPassword}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		h.clock.Advance(16 * time.Minute)

		_, err = h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupToken: token, SetupID: begun.SetupID, TOTPCode: h.code(begun.TOTP.Secret)}, testClient())

		if !errors.Is(err, auth.ErrSetupExpired) {
			t.Errorf("CompleteSetup error = %v, want ErrSetupExpired", err)
		}
	})

	t.Run("gives an authenticator QR code and otpauth URL", func(t *testing.T) {
		h := newHarness(t)
		token, _ := h.service.PrepareSetupToken(t.Context()) // checked by the tests above
		begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: token, Username: "admin", Password: strongPassword}, testClient())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(begun.TOTP.URL, "otpauth://totp/Sentinel:admin") || !strings.Contains(begun.TOTP.QRCode, "data:image/png;base64,") {
			t.Errorf("enrollment = %+v, want an otpauth URL and a PNG data URL", begun.TOTP)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

func testStart() time.Time {
	return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
}

func TestBackoffFor(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: 0},
		{failures: 2, want: 0},
		{failures: 3, want: time.Second},
		{failures: 4, want: 2 * time.Second},
		{failures: 5, want: 4 * time.Second},
		{failures: 12, want: 512 * time.Second},
		{failures: 13, want: maxBackoff},
		{failures: 50, want: maxBackoff},
	}
	for _, test := range tests {
		if got := backoffFor(test.failures); got != test.want {
			t.Errorf("backoffFor(%d) = %v, want %v", test.failures, got, test.want)
		}
	}
}

func TestLimiter(t *testing.T) {
	t.Run("forgets failures after an hour without one", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		limits := newLimiter(clock, false)
		for range 5 {
			limits.RecordFailure("key")
		}
		clock.Advance(2 * time.Hour)
		limits.RecordFailure("key")

		if wait := limits.RetryAfter("key"); wait != 0 {
			t.Errorf("RetryAfter = %v, want 0 after the count was forgotten", wait)
		}
	})

	t.Run("never locks without lockout enabled", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		limits := newLimiter(clock, false)
		for range 20 {
			if limits.RecordFailure("key") {
				t.Fatal("RecordFailure reported a lock")
			}
		}
	})

	t.Run("reset clears a key", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		limits := newLimiter(clock, true)
		for range 5 {
			limits.RecordFailure("key")
		}
		limits.Reset("key")
		if wait := limits.RetryAfter("key"); wait != 0 {
			t.Errorf("RetryAfter after Reset = %v, want 0", wait)
		}
	})

	t.Run("drops stale keys when it tracks too many", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		limits := newLimiter(clock, false)
		for i := range maxTrackedKeys {
			limits.failures["stale-"+strconv.Itoa(i)] = &failureRecord{count: 1, lastFailure: clock.Now()}
		}
		clock.Advance(2 * time.Hour)

		limits.RecordFailure("fresh")

		if len(limits.failures) != 1 {
			t.Errorf("tracking %d keys, want only the fresh one", len(limits.failures))
		}
	})

	t.Run("keeps a locked key while dropping stale ones", func(t *testing.T) {
		clock := fakeclock.New(testStart())
		limits := newLimiter(clock, true)
		limits.failures["locked"] = &failureRecord{lastFailure: clock.Now(), lockedUntil: clock.Now().Add(3 * time.Hour)}
		clock.Advance(2 * time.Hour)

		limits.forgetStale(clock.Now())

		if _, kept := limits.failures["locked"]; !kept {
			t.Error("a key that is still locked was forgotten")
		}
	})
}

func TestParseArgon2Hash(t *testing.T) {
	valid := encodeArgon2Hash([]byte("0123456789abcdef"), []byte("0123456789abcdef0123456789abcdef"))
	if _, err := parseArgon2Hash(valid); err != nil {
		t.Fatalf("parseArgon2Hash(valid) returned %v", err)
	}

	malformed := []string{
		"",
		"plain text",
		"$argon2i$v=19$m=65536,t=3,p=2$c2FsdA$a2V5",
		"$argon2id$v=18$m=65536,t=3,p=2$c2FsdA$a2V5",
		"$argon2id$v=19$m=x,t=3,p=2$c2FsdA$a2V5",
		"$argon2id$v=19$m=65536,t=3,p=2$!!!$a2V5",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$!!!",
		"$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$",
		"$argon2id$v=19$m=0,t=3,p=2$c2FsdA$a2V5",
	}
	for _, encoded := range malformed {
		if _, err := parseArgon2Hash(encoded); err == nil {
			t.Errorf("parseArgon2Hash(%q) returned nil, want an error", encoded)
		}
	}
}

func TestPasswordHasher(t *testing.T) {
	hasher := newPasswordHasher()

	t.Run("verifies a password against its hash", func(t *testing.T) {
		hash, err := hasher.Hash(t.Context(), "a long enough password")
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := hasher.Verify(t.Context(), "a long enough password", hash); !ok || err != nil {
			t.Errorf("Verify(right) = %v, %v; want true", ok, err)
		}
		if ok, _ := hasher.Verify(t.Context(), "another password", hash); ok { // the error case is tested below
			t.Error("Verify(wrong) = true")
		}
	})

	t.Run("never matches when there is no stored hash", func(t *testing.T) {
		if ok, err := hasher.Verify(t.Context(), "anything at all", ""); ok || err != nil {
			t.Errorf("Verify against nothing = %v, %v; want false, nil", ok, err)
		}
	})

	t.Run("reports a malformed stored hash", func(t *testing.T) {
		if _, err := hasher.Verify(t.Context(), "anything", "garbage"); err == nil {
			t.Error("Verify returned nil for a malformed hash")
		}
	})

	t.Run("gives up waiting when the context ends", func(t *testing.T) {
		busy := newPasswordHasher()
		for range maxConcurrentHashes {
			busy.slots <- struct{}{}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if _, err := busy.Hash(ctx, "a long enough password"); err == nil {
			t.Error("Hash returned nil while every slot was busy and the context ended")
		}
		if _, err := busy.Verify(ctx, "a long enough password", busy.dummyHash); err == nil {
			t.Error("Verify returned nil while every slot was busy and the context ended")
		}
	})
}

func TestVerifyTOTP(t *testing.T) {
	enrollment, err := newTOTPEnrollment("alice")
	if err != nil {
		t.Fatal(err)
	}
	now := testStart()
	step := now.Unix() / 30
	codeAt := func(at time.Time) string {
		code, _ := generateCodeForTest(enrollment.Secret, at) // a valid secret can't fail
		return code
	}

	tests := []struct {
		name         string
		code         string
		lastUsedStep int64
		wantOK       bool
		wantStep     int64
	}{
		{name: "accepts the current code", code: codeAt(now), wantOK: true, wantStep: step},
		{name: "accepts the previous step's code", code: codeAt(now.Add(-30 * time.Second)), wantOK: true, wantStep: step - 1},
		{name: "accepts the next step's code", code: codeAt(now.Add(30 * time.Second)), wantOK: true, wantStep: step + 1},
		{name: "refuses a code two steps old", code: codeAt(now.Add(-60 * time.Second)), wantOK: false},
		{name: "refuses a code already used", code: codeAt(now), lastUsedStep: step, wantOK: false},
		{name: "refuses a short code", code: "12345", wantOK: false},
		{name: "refuses a long code", code: "1234567", wantOK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotStep, ok := verifyTOTP(enrollment.Secret, test.code, now, test.lastUsedStep)
			if ok != test.wantOK || (ok && gotStep != test.wantStep) {
				t.Errorf("verifyTOTP = %d, %v; want %d, %v", gotStep, ok, test.wantStep, test.wantOK)
			}
		})
	}

	t.Run("refuses every code for a secret that isn't base32", func(t *testing.T) {
		if _, ok := verifyTOTP("not base32!", "123456", now, 0); ok {
			t.Error("verifyTOTP accepted a code for a malformed secret")
		}
	})

	t.Run("refuses every code before the Unix epoch", func(t *testing.T) {
		if _, ok := verifyTOTP(enrollment.Secret, "123456", time.Unix(-100, 0), 0); ok {
			t.Error("verifyTOTP accepted a code at a negative time")
		}
	})
}

func TestPermissions(t *testing.T) {
	tests := []struct {
		permission Permission
		viewer     bool
		operator   bool
		admin      bool
	}{
		{PermissionViewFleet, true, true, true},
		{PermissionUseTerminal, false, true, true},
		{PermissionUseDesktop, false, true, true},
		{PermissionManageFiles, false, true, true},
		{PermissionManageServices, false, true, true},
		{PermissionManageDocker, false, true, true},
		{PermissionRunScripts, false, true, true},
		{PermissionManageUsers, false, false, true},
		{PermissionManageEnrollment, false, false, true},
		{PermissionManageMachines, false, false, true},
		{PermissionRevokeAgents, false, false, true},
		{PermissionManageAlerts, false, false, true},
		{PermissionManageSettings, false, false, true},
		{PermissionViewAuditLog, false, false, true},
		{Permission("launch_missiles"), false, false, false},
	}
	for _, test := range tests {
		t.Run(string(test.permission), func(t *testing.T) {
			if RoleViewer.Can(test.permission) != test.viewer || RoleOperator.Can(test.permission) != test.operator ||
				RoleAdmin.Can(test.permission) != test.admin {
				t.Errorf("viewer/operator/admin = %v/%v/%v, want %v/%v/%v", RoleViewer.Can(test.permission),
					RoleOperator.Can(test.permission), RoleAdmin.Can(test.permission), test.viewer, test.operator, test.admin)
			}
		})
	}

	t.Run("an unknown role can do nothing", func(t *testing.T) {
		if Role("root").Can(PermissionViewFleet) || Role("").IsValid() {
			t.Error("an unknown role was given a permission")
		}
	})
}

func TestPasswordPolicy(t *testing.T) {
	policy := newPasswordPolicy()
	if len(policy.common) < 40_000 {
		t.Errorf("loaded %d common passwords, want the full list", len(policy.common))
	}
	if err := policy.Check("alice", "an unusual passphrase"); err != nil {
		t.Errorf("Check(good) = %v", err)
	}
	if err := policy.Check("alice", strings.Repeat("é", 12)); err != nil {
		t.Errorf("Check counts characters, not bytes: %v", err)
	}
}

func TestSetupTokenFile(t *testing.T) {
	t.Run("reports a token path that can't be read", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "setup-token")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := readOrCreateSetupToken(directory); err == nil {
			t.Error("readOrCreateSetupToken returned nil for a directory")
		}
		if setupTokenMatches(directory, "anything") {
			t.Error("a directory matched as a token")
		}
	})

	t.Run("reports a token that can't be written", func(t *testing.T) {
		if _, err := readOrCreateSetupToken(filepath.Join(t.TempDir(), "missing", "setup-token")); err == nil {
			t.Error("readOrCreateSetupToken returned nil for a missing folder")
		}
	})

	t.Run("an empty token file never matches", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "setup-token")
		if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if setupTokenMatches(path, "") {
			t.Error("an empty token matched an empty file")
		}
	})

	t.Run("reports a token file that can't be removed", func(t *testing.T) {
		directory := t.TempDir()
		path := filepath.Join(directory, "setup-token")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "inside"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := removeSetupToken(path); err == nil {
			t.Error("removeSetupToken returned nil for a non-empty directory")
		}
	})
}

func TestPendingSetups(t *testing.T) {
	clock := fakeclock.New(testStart())
	pending := newPendingSetups(clock)
	old := pending.Add(pendingSetup{username: "old"})
	clock.Advance(20 * time.Minute)

	fresh := pending.Add(pendingSetup{username: "fresh"})

	if _, found := pending.Get(old); found {
		t.Error("an expired setup was returned")
	}
	if len(pending.pending) != 1 {
		t.Errorf("holding %d setups, want the expired one dropped", len(pending.pending))
	}
	if setup, found := pending.Get(fresh); !found || setup.username != "fresh" {
		t.Errorf("Get(fresh) = %+v, %v", setup, found)
	}
}

func TestErrorMessages(t *testing.T) {
	tooMany := &TooManyAttemptsError{RetryAfter: 90 * time.Second}
	if !strings.Contains(tooMany.Error(), "1m30s") {
		t.Errorf("TooManyAttemptsError = %q, want the wait", tooMany.Error())
	}
	validation := &ValidationError{Field: "username", Message: "is wrong"}
	if validation.Error() != "username: is wrong" {
		t.Errorf("ValidationError = %q", validation.Error())
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short) = %q", got)
	}
	if got := truncate("abcdé", 5); got != "abcd" {
		t.Errorf("truncate splitting a character = %q, want the broken character dropped", got)
	}
}

// generateCodeForTest computes a TOTP code with the library directly, as an
// independent check of verifyTOTP.
func generateCodeForTest(secret string, at time.Time) (string, error) {
	return totp.GenerateCodeCustom(secret, at, totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
}

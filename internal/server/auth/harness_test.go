// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/secrets"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// strongPassword passes the password policy.
const strongPassword = "correct horse battery staple"

// harness is a Service on a real database in a temporary directory, with a
// fake clock.
type harness struct {
	service  *auth.Service
	clock    *fakeclock.Clock
	database *faultyDatabase
	auditLog *audit.Log
	dataDir  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dataDir := t.TempDir()
	clock := fakeclock.New(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(dataDir, "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() }) // nothing to check when a test database closes
	box, err := secrets.LoadOrCreateKey(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	auditLog := audit.New(database)
	faulty := &faultyDatabase{store: database}
	return &harness{
		service:  newService(faulty, box, auditLog, clock, dataDir),
		clock:    clock,
		database: faulty,
		auditLog: auditLog,
		dataDir:  dataDir,
	}
}

func newService(database auth.Database, box *secrets.Box, auditLog *audit.Log, clock *fakeclock.Clock, dataDir string) *auth.Service {
	return auth.NewService(auth.Options{Database: database, Secrets: box, AuditLog: auditLog, Clock: clock, DataDir: dataDir})
}

// restart returns a new Service on the same database and data directory, as
// after a server restart: in-memory state such as rate limits is gone.
func (h *harness) restart(t *testing.T) *auth.Service {
	t.Helper()
	box, err := secrets.LoadOrCreateKey(h.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return newService(h.database, box, h.auditLog, h.clock, h.dataDir)
}

// testClient is the client used by most tests.
func testClient() auth.RequestInfo {
	return auth.RequestInfo{IP: "203.0.113.7", UserAgent: "test-browser"}
}

// account is a user the tests can sign in as.
type account struct {
	username   string
	password   string
	totpSecret string
}

// setUpAdmin completes first-boot setup and returns the admin, a signed-in
// session token and the recovery codes.
func (h *harness) setUpAdmin(t *testing.T) (account, string, []string) {
	t.Helper()
	token, err := h.service.PrepareSetupToken(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: token, Username: "admin", Password: strongPassword}, testClient())
	if err != nil {
		t.Fatalf("BeginSetup returned %v", err)
	}
	signedIn, err := h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{
		SetupToken: token, SetupID: begun.SetupID, TOTPCode: h.code(begun.TOTP.Secret),
	}, testClient())
	if err != nil {
		t.Fatalf("CompleteSetup returned %v", err)
	}
	// The setup code's 30-second step can't be used again; move on so the
	// next code is fresh.
	h.clock.Advance(30 * time.Second)
	return account{username: "admin", password: strongPassword, totpSecret: begun.TOTP.Secret}, signedIn.SessionToken, signedIn.RecoveryCodes
}

// signIn does both login steps and returns the signed-in session token.
func (h *harness) signIn(t *testing.T, user account) string {
	t.Helper()
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: user.username, Password: user.password}, testClient())
	if err != nil {
		t.Fatalf("Login returned %v", err)
	}
	session := h.authenticate(t, result.SessionToken)
	signedIn, err := h.service.VerifySecondFactor(t.Context(), session, auth.SecondFactorInput{TOTPCode: h.code(user.totpSecret)}, testClient())
	if err != nil {
		t.Fatalf("VerifySecondFactor returned %v", err)
	}
	h.clock.Advance(30 * time.Second)
	return signedIn.SessionToken
}

// createUser adds a user as admin and has them enroll TOTP, returning the
// account.
func (h *harness) createUser(t *testing.T, adminToken, username string, role auth.Role) account {
	t.Helper()
	admin := h.authenticate(t, adminToken)
	if _, err := h.service.CreateUser(t.Context(), admin, auth.CreateUserInput{Username: username, Password: strongPassword, Role: role}, testClient()); err != nil {
		t.Fatalf("CreateUser returned %v", err)
	}
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: username, Password: strongPassword}, testClient())
	if err != nil {
		t.Fatal(err)
	}
	pending := h.authenticate(t, result.SessionToken)
	enrollment, err := h.service.BeginTOTPEnrollment(t.Context(), pending)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.ConfirmTOTPEnrollment(t.Context(), pending, h.code(enrollment.Secret), testClient()); err != nil {
		t.Fatal(err)
	}
	h.clock.Advance(30 * time.Second)
	return account{username: username, password: strongPassword, totpSecret: enrollment.Secret}
}

func (h *harness) authenticate(t *testing.T, token string) auth.Session {
	t.Helper()
	session, err := h.service.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatalf("Authenticate returned %v", err)
	}
	return session
}

// code returns the TOTP code for secret at the fake clock's time.
func (h *harness) code(secret string) string {
	return totpCodeAt(secret, h.clock.Now())
}

// totpCodeAt computes a TOTP code independently of the code under test.
func totpCodeAt(secret string, at time.Time) string {
	step := uint64(at.Unix() / 30) //nolint:gosec // G115: test times are in 2026, far from overflow
	// The only error is a malformed secret, which then fails the test's
	// own assertion because the empty code is never accepted.
	code, _ := hotp.GenerateCodeCustom(secret, step, hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	return code
}

// auditActions returns every audit entry as "action:outcome", oldest first.
func (h *harness) auditActions(t *testing.T) []string {
	t.Helper()
	var actions []string
	err := h.auditLog.All(t.Context(), func(record audit.Record) error {
		actions = append(actions, record.Action+":"+string(record.Outcome))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return actions
}

// queryString reads one string value from the database.
func (h *harness) queryString(t *testing.T, query string, args ...any) string {
	t.Helper()
	var value string
	err := h.database.store.Read(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), query, args...).Scan(&value)
	})
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return value
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func countOf(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}

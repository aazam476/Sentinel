// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/store"
)

// errInjected is what faultyDatabase returns when told to fail.
var errInjected = errors.New("injected database failure")

// faultyDatabase passes everything to a real store, but can be told to fail
// every read, to check that the service reports database failures instead
// of carrying on. Write failures are injected with triggers instead (see
// failStatement), so they hit one statement at a time.
type faultyDatabase struct {
	store     *store.Store
	failReads atomic.Bool
}

func (d *faultyDatabase) Read(ctx context.Context, work func(tx *sql.Tx) error) error {
	if d.failReads.Load() {
		return errInjected
	}
	return d.store.Read(ctx, work)
}

func (d *faultyDatabase) Write(ctx context.Context, work func(tx *sql.Tx) error) error {
	return d.store.Write(ctx, work)
}

// failStatement makes every statement of kind ("INSERT", "UPDATE" or
// "DELETE") on table fail, using a trigger, until the test ends.
func (h *harness) failStatement(t *testing.T, kind, table string) {
	t.Helper()
	name := "inject_" + kind + "_" + table
	err := h.database.store.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `CREATE TRIGGER `+name+` BEFORE `+kind+` ON `+table+
			` BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
		return err
	})
	if err != nil {
		t.Fatalf("inject fault: %v", err)
	}
	t.Cleanup(func() {
		_ = h.database.store.Write(context.Background(), func(tx *sql.Tx) error { // best effort; the database is discarded anyway
			_, err := tx.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS `+name)
			return err
		})
	})
}

func TestDatabaseFailuresAreReported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		fault func(t *testing.T, h *harness)
		// run prepares what the operation needs, calls inject to switch the
		// fault on, then does the operation under test and returns its error.
		run func(t *testing.T, h *harness, f fixture, inject func()) error
	}{
		{name: "setup required check", fault: failReads, run: func(t *testing.T, h *harness, _ fixture, inject func()) error {
			inject()
			_, err := h.service.SetupRequired(t.Context())
			return err
		}},
		{name: "preparing the setup token", fault: failReads, run: func(t *testing.T, h *harness, _ fixture, inject func()) error {
			inject()
			_, err := h.service.PrepareSetupToken(t.Context())
			return err
		}},
		{name: "login user lookup", fault: failReads, run: adminPasswordLogin},
		{name: "login session creation", fault: failing("INSERT", "sessions"), run: adminPasswordLogin},
		{name: "login audit entry", fault: failing("INSERT", "audit_log"), run: adminPasswordLogin},
		{name: "failed login audit entry", fault: failing("INSERT", "audit_log"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			_, err := h.service.Login(t.Context(), auth.LoginInput{Username: f.admin.username, Password: "wrong password!!"}, testClient())
			return expectNot(err, auth.ErrInvalidCredentials)
		}},
		{name: "second factor TOTP step record", fault: failing("UPDATE", "totp_secrets"), run: secondFactorWithTOTP},
		{name: "second factor session replacement", fault: failing("DELETE", "sessions"), run: secondFactorWithTOTP},
		{name: "second factor new session", fault: failing("INSERT", "sessions"), run: secondFactorWithTOTP},
		{name: "second factor lock reset", fault: failing("UPDATE", "users"), run: secondFactorWithTOTP},
		{name: "second factor audit entry", fault: failing("INSERT", "audit_log"), run: secondFactorWithTOTP},
		{name: "second factor TOTP secret read", fault: failReadOf("totp_secrets"), run: secondFactorWithTOTP},
		{name: "recovery code use", fault: failing("UPDATE", "recovery_codes"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			pending := f.pending(t, h)
			inject()
			_, err := h.service.VerifySecondFactor(t.Context(), pending, auth.SecondFactorInput{RecoveryCode: f.recoveryCodes[0]}, testClient())
			return err
		}},
		{name: "authenticate lookup", fault: failReads, run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			_, err := h.service.Authenticate(t.Context(), f.adminToken)
			return expectNot(err, auth.ErrNotAuthenticated)
		}},
		{name: "authenticate last-seen update", fault: failing("UPDATE", "sessions"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			h.clock.Advance(2 * time.Minute)
			inject()
			_, err := h.service.Authenticate(t.Context(), f.adminToken)
			return err
		}},
		{name: "logout", fault: failing("DELETE", "sessions"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			return h.service.Logout(t.Context(), f.adminSession, testClient())
		}},
		{name: "list sessions", fault: failReads, run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			_, err := h.service.ListSessions(t.Context(), f.adminSession)
			return err
		}},
		{name: "revoke session", fault: failing("DELETE", "sessions"), run: revokeOwnSession},
		{name: "revoke session audit", fault: failing("INSERT", "audit_log"), run: revokeOwnSession},
		{name: "step-up session update", fault: failing("UPDATE", "sessions"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			return h.service.StepUp(t.Context(), f.adminSession, h.code(f.admin.totpSecret), testClient())
		}},
		{name: "step-up failure audit", fault: failing("INSERT", "audit_log"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			return expectNot(h.service.StepUp(t.Context(), f.adminSession, "000000", testClient()), auth.ErrInvalidCode)
		}},
		{name: "list users", fault: failReads, run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			_, err := h.service.ListUsers(t.Context(), f.adminSession)
			return err
		}},
		{name: "create user", fault: failing("INSERT", "users"), run: createViewer},
		{name: "create user audit", fault: failing("INSERT", "audit_log"), run: createViewer},
		{name: "change role", fault: failing("UPDATE", "users"), run: makeViewerOperator},
		{name: "change role audit", fault: failing("INSERT", "audit_log"), run: makeViewerOperator},
		{name: "disable user sessions", fault: failing("DELETE", "sessions"), run: disableViewer},
		{name: "disable user", fault: failing("UPDATE", "users"), run: disableViewer},
		{name: "begin enrollment", fault: failing("INSERT", "totp_secrets"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			pending := f.newcomerPending(t, h)
			inject()
			_, err := h.service.BeginTOTPEnrollment(t.Context(), pending)
			return err
		}},
		{name: "confirm enrollment", fault: failing("UPDATE", "totp_secrets"), run: confirmNewcomer},
		{name: "enrollment recovery codes", fault: failing("INSERT", "recovery_codes"), run: confirmNewcomer},
		{name: "enrollment session", fault: failing("INSERT", "sessions"), run: confirmNewcomer},
		{name: "enrollment audit", fault: failing("INSERT", "audit_log"), run: confirmNewcomer},
		{name: "login 2FA check", fault: failReadOf("totp_secrets"), run: adminPasswordLogin},
		{name: "list sessions query", fault: failReadOf("sessions"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			_, err := h.service.ListSessions(t.Context(), f.adminSession)
			return err
		}},
		{name: "list users query", fault: failReadOf("users"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			inject()
			_, err := h.service.ListUsers(t.Context(), f.adminSession)
			return err
		}},
		{name: "account lock", fault: failing("UPDATE", "users"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			return failUntilLocked(t, h, f, inject)
		}},
		{name: "account lock audit", fault: failing("INSERT", "audit_log"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			return failUntilLocked(t, h, f, inject)
		}},
		{name: "rate limit refusal audit", fault: failing("INSERT", "audit_log"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			wrong := auth.LoginInput{Username: f.admin.username, Password: "wrong password!!"}
			for range 3 {
				_, _ = h.service.Login(t.Context(), wrong, testClient()) // building up backoff; failures expected
			}
			inject()
			_, err := h.service.Login(t.Context(), wrong, testClient())
			var tooMany *auth.TooManyAttemptsError
			if errors.As(err, &tooMany) {
				return nil
			}
			return err
		}},
		{name: "locked account refusal audit", fault: failing("INSERT", "audit_log"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			if err := failUntilLocked(t, h, f, func() {}); err != nil {
				t.Fatalf("the account did not lock: %v", err)
			}
			inject()
			_, err := h.restart(t).Login(t.Context(), auth.LoginInput{Username: f.admin.username, Password: f.admin.password}, auth.RequestInfo{IP: "192.0.2.200"})
			var tooMany *auth.TooManyAttemptsError
			if errors.As(err, &tooMany) {
				return nil
			}
			return err
		}},
		{name: "enrollment failure audit", fault: failing("INSERT", "audit_log"), run: func(t *testing.T, h *harness, f fixture, inject func()) error {
			pending := f.newcomerPending(t, h)
			if _, err := h.service.BeginTOTPEnrollment(t.Context(), pending); err != nil {
				t.Fatal(err)
			}
			inject()
			_, err := h.service.ConfirmTOTPEnrollment(t.Context(), pending, "000000", testClient())
			return expectNot(err, auth.ErrInvalidCode)
		}},
	}

	for _, test := range tests {
		t.Run("reports a failure in "+test.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			fixture := newFixture(t, h)

			err := test.run(t, h, fixture, func() { test.fault(t, h) })

			if err == nil {
				t.Error("returned nil, want the database failure")
			}
		})
	}
}

// fixture is the state most fault tests start from.
type fixture struct {
	admin         account
	adminToken    string
	adminSession  auth.Session
	recoveryCodes []string
}

func newFixture(t *testing.T, h *harness) fixture {
	t.Helper()
	admin, token, codes := h.setUpAdmin(t)
	viewer := auth.CreateUserInput{Username: "viewer", Password: strongPassword, Role: auth.RoleViewer}
	newcomer := auth.CreateUserInput{Username: "newcomer", Password: strongPassword, Role: auth.RoleViewer}
	session := h.authenticate(t, token)
	for _, input := range []auth.CreateUserInput{viewer, newcomer} {
		if _, err := h.service.CreateUser(t.Context(), session, input, testClient()); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{admin: admin, adminToken: token, adminSession: session, recoveryCodes: codes}
}

// pending returns a fresh password-only session for the admin.
func (f fixture) pending(t *testing.T, h *harness) auth.Session {
	t.Helper()
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: f.admin.username, Password: f.admin.password}, auth.RequestInfo{IP: "192.0.2.77"})
	if err != nil {
		t.Fatalf("Login returned %v", err)
	}
	return h.authenticate(t, result.SessionToken)
}

// newcomerPending returns a password-only session for a user without 2FA.
func (f fixture) newcomerPending(t *testing.T, h *harness) auth.Session {
	t.Helper()
	result, err := h.service.Login(t.Context(), auth.LoginInput{Username: "newcomer", Password: strongPassword}, auth.RequestInfo{IP: "192.0.2.78"})
	if err != nil {
		t.Fatalf("Login returned %v", err)
	}
	return h.authenticate(t, result.SessionToken)
}

func (f fixture) viewerID(t *testing.T, h *harness) int64 {
	t.Helper()
	return findUser(t, h, f.adminSession, "viewer").ID
}

func failReads(_ *testing.T, h *harness) { h.database.failReads.Store(true) }

// failing returns a fault that makes every statement of kind ("INSERT",
// "UPDATE" or "DELETE") on table fail.
func failing(kind, table string) func(t *testing.T, h *harness) {
	return func(t *testing.T, h *harness) { h.failStatement(t, kind, table) }
}

// failReadOf makes reading a table fail by renaming it away.
func failReadOf(table string) func(t *testing.T, h *harness) {
	return func(t *testing.T, h *harness) {
		t.Helper()
		err := h.database.store.Write(t.Context(), func(tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), `ALTER TABLE `+table+` RENAME TO `+table+`_hidden`)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// failUntilLocked fails the admin's password ten times from different IP
// addresses, switching the fault on before the tenth (locking) failure. It
// returns the tenth failure's error, or nil when that was the expected lock.
func failUntilLocked(t *testing.T, h *harness, f fixture, inject func()) error {
	wrong := auth.LoginInput{Username: f.admin.username, Password: "wrong password!!"}
	for attempt := 1; attempt <= 10; attempt++ {
		h.clock.Advance(16 * time.Minute)
		if attempt == 10 {
			inject()
		}
		_, err := h.service.Login(t.Context(), wrong, auth.RequestInfo{IP: "198.51.100." + strconv.Itoa(attempt)})
		if attempt == 10 {
			var tooMany *auth.TooManyAttemptsError
			if errors.As(err, &tooMany) {
				return nil
			}
			return err
		}
	}
	return nil
}

func adminPasswordLogin(t *testing.T, h *harness, f fixture, inject func()) error {
	inject()
	_, err := h.service.Login(t.Context(), auth.LoginInput{Username: f.admin.username, Password: f.admin.password}, testClient())
	return err
}

func secondFactorWithTOTP(t *testing.T, h *harness, f fixture, inject func()) error {
	pending := f.pending(t, h)
	inject()
	_, err := h.service.VerifySecondFactor(t.Context(), pending, auth.SecondFactorInput{TOTPCode: h.code(f.admin.totpSecret)}, testClient())
	return expectNot(err, auth.ErrInvalidCode)
}

func revokeOwnSession(t *testing.T, h *harness, f fixture, inject func()) error {
	inject()
	return h.service.RevokeSession(t.Context(), f.adminSession, f.adminSession.ID, testClient())
}

func createViewer(t *testing.T, h *harness, f fixture, inject func()) error {
	inject()
	_, err := h.service.CreateUser(t.Context(), f.adminSession, auth.CreateUserInput{Username: "someone", Password: strongPassword, Role: auth.RoleViewer}, testClient())
	return err
}

func makeViewerOperator(t *testing.T, h *harness, f fixture, inject func()) error {
	viewerID := f.viewerID(t, h)
	inject()
	operator := auth.RoleOperator
	_, err := h.service.UpdateUser(t.Context(), f.adminSession, viewerID, auth.UpdateUserInput{Role: &operator}, testClient())
	return err
}

func disableViewer(t *testing.T, h *harness, f fixture, inject func()) error {
	viewerID := f.viewerID(t, h)
	// Give the viewer a session, so disabling them has one to delete.
	if _, err := h.service.Login(t.Context(), auth.LoginInput{Username: "viewer", Password: strongPassword}, auth.RequestInfo{IP: "192.0.2.79"}); err != nil {
		t.Fatal(err)
	}
	inject()
	disabled := true
	_, err := h.service.UpdateUser(t.Context(), f.adminSession, viewerID, auth.UpdateUserInput{Disabled: &disabled}, testClient())
	return err
}

func confirmNewcomer(t *testing.T, h *harness, f fixture, inject func()) error {
	pending := f.newcomerPending(t, h)
	enrollment, err := h.service.BeginTOTPEnrollment(t.Context(), pending)
	if err != nil {
		t.Fatal(err)
	}
	inject()
	_, err = h.service.ConfirmTOTPEnrollment(t.Context(), pending, h.code(enrollment.Secret), testClient())
	return err
}

// expectNot turns the expected, non-database error into nil, so the test
// only passes when the injected failure surfaced instead.
func expectNot(err, ordinary error) error {
	if errors.Is(err, ordinary) {
		return nil
	}
	return err
}

func TestSetupDatabaseFailuresAreReported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		fault func(t *testing.T, h *harness)
		// wrongToken uses an incorrect setup token, to reach the failure audit.
		wrongToken bool
		// atBegin injects the fault before BeginSetup instead of before
		// CompleteSetup.
		atBegin bool
	}{
		{name: "the user count at the start", fault: failReads, atBegin: true},
		{name: "the user count query", fault: failReadOf("users"), atBegin: true},
		{name: "the audit of a wrong token", fault: failing("INSERT", "audit_log"), wrongToken: true, atBegin: true},
		{name: "creating the admin", fault: failing("INSERT", "users")},
		{name: "saving the TOTP secret", fault: failing("INSERT", "totp_secrets")},
		{name: "recording the used TOTP step", fault: failing("UPDATE", "totp_secrets")},
		{name: "saving recovery codes", fault: failing("INSERT", "recovery_codes")},
		{name: "starting the session", fault: failing("INSERT", "sessions")},
		{name: "auditing the setup", fault: failing("INSERT", "audit_log")},
	}

	for _, test := range tests {
		t.Run("reports a failure in "+test.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			token, err := h.service.PrepareSetupToken(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if test.wrongToken {
				token = "WRONG"
			}
			if test.atBegin {
				test.fault(t, h)
			}

			err = runSetup(t, h, token, func() {
				if !test.atBegin {
					test.fault(t, h)
				}
			})

			if err == nil || errors.Is(err, auth.ErrInvalidSetupToken) {
				t.Errorf("setup error = %v, want the database failure", err)
			}
		})
	}
}

// runSetup does both setup steps, calling inject between them.
func runSetup(t *testing.T, h *harness, token string, inject func()) error {
	begun, err := h.service.BeginSetup(t.Context(), auth.BeginSetupInput{SetupToken: token, Username: "admin", Password: strongPassword}, testClient())
	if err != nil {
		return err
	}
	inject()
	_, err = h.service.CompleteSetup(t.Context(), auth.CompleteSetupInput{SetupToken: token, SetupID: begun.SetupID, TOTPCode: h.code(begun.TOTP.Secret)}, testClient())
	return err
}

// SPDX-License-Identifier: Apache-2.0

package fleet_test

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// harness is a fleet Service on a real database with a fake clock, plus an
// admin, an operator and a viewer to act as.
type harness struct {
	service  *fleet.Service
	clock    *fakeclock.Clock
	store    *store.Store
	database *faultyDatabase
	auditLog *audit.Log
	admin    auth.Session
	operator auth.Session
	viewer   auth.Session
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() }) // nothing to check when a test database closes
	auditLog := audit.New(database)
	faulty := &faultyDatabase{store: database}
	h := &harness{
		service:  fleet.NewService(fleet.Options{Database: faulty, AuditLog: auditLog, Clock: clock}),
		clock:    clock,
		store:    database,
		database: faulty,
		auditLog: auditLog,
	}
	h.admin = h.addUser(t, "alice", auth.RoleAdmin)
	h.operator = h.addUser(t, "olu", auth.RoleOperator)
	h.viewer = h.addUser(t, "vic", auth.RoleViewer)
	return h
}

// addUser stores a user and returns a signed-in session for them.
func (h *harness) addUser(t *testing.T, username string, role auth.Role) auth.Session {
	t.Helper()
	var id int64
	err := h.store.Write(t.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(t.Context(), `INSERT INTO users (username, password_hash, role, created_at) VALUES (?, 'x', ?, 0)`,
			username, string(role))
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	signedInAt := int64(0)
	return auth.Session{UserID: id, Username: username, Role: role, SecondFactorAt: &signedInAt}
}

// browser is where the tests' admin requests come from.
func browser() auth.RequestInfo {
	return auth.RequestInfo{IP: "203.0.113.7", UserAgent: "test-browser"}
}

// newToken creates a token with the defaults.
func (h *harness) newToken(t *testing.T) fleet.CreatedToken {
	t.Helper()
	token, err := h.service.CreateEnrollmentToken(t.Context(), h.admin, fleet.CreateTokenInput{Note: "office NAS"}, browser())
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// enrollInput is what a typical agent sends, with a fresh key.
func enrollInput(t *testing.T, token string) fleet.EnrollInput {
	t.Helper()
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return fleet.EnrollInput{
		Token: token, PublicKey: public, Hostname: "nas-01", OS: "linux", Arch: "amd64",
		AgentVersion: "v0.1.0", ProtocolVersion: 1, IP: "198.51.100.20",
	}
}

// enroll enrolls a new agent with a new token.
func (h *harness) enroll(t *testing.T, hostname string) fleet.Agent {
	t.Helper()
	input := enrollInput(t, h.newToken(t).Token)
	input.Hostname = hostname
	agent, err := h.service.Enroll(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

// auditActions returns the action and outcome of every audit entry, oldest
// first.
func (h *harness) auditActions(t *testing.T) []string {
	t.Helper()
	records, err := h.auditLog.List(t.Context(), audit.ListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	actions := make([]string, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		actions = append(actions, records[i].Action+":"+string(records[i].Outcome))
	}
	return actions
}

// lastAudit returns the newest audit entry.
func (h *harness) lastAudit(t *testing.T) audit.Record {
	t.Helper()
	records, err := h.auditLog.List(t.Context(), audit.ListOptions{Limit: 1})
	if err != nil || len(records) == 0 {
		t.Fatalf("no audit entry (%v)", err)
	}
	return records[0]
}

// errInjected is what faultyDatabase returns when told to fail.
var errInjected = errors.New("injected database failure")

// faultyDatabase passes everything to a real store, but can be told to fail
// every read or every write.
type faultyDatabase struct {
	store      *store.Store
	failReads  atomic.Bool
	failWrites atomic.Bool
}

func (d *faultyDatabase) Read(ctx context.Context, work func(tx *sql.Tx) error) error {
	if d.failReads.Load() {
		return errInjected
	}
	return d.store.Read(ctx, work)
}

func (d *faultyDatabase) Write(ctx context.Context, work func(tx *sql.Tx) error) error {
	if d.failWrites.Load() {
		return errInjected
	}
	return d.store.Write(ctx, work)
}

// failStatement makes every statement of kind ("INSERT", "UPDATE" or
// "DELETE") on table fail, using a trigger, until the test ends.
func (h *harness) failStatement(t *testing.T, kind, table string) {
	t.Helper()
	name := "inject_" + kind + "_" + table
	h.exec(t, `CREATE TRIGGER `+name+` BEFORE `+kind+` ON `+table+` BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	t.Cleanup(func() {
		_ = h.store.Write(context.Background(), func(tx *sql.Tx) error { // best effort; the database is discarded anyway
			_, err := tx.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS `+name)
			return err
		})
	})
}

// exec runs one statement directly on the database.
func (h *harness) exec(t *testing.T, statement string) {
	t.Helper()
	err := h.store.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), statement)
		return err
	})
	if err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

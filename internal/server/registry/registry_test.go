// SPDX-License-Identifier: Apache-2.0

package registry_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/registry"
	"github.com/aazam476/sentinel/internal/server/secrets"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

type harness struct {
	service  *registry.Service
	store    *store.Store
	auditLog *audit.Log
	admin    auth.Session
	agentID  int64
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() }) // nothing to check when a test database closes
	box, err := secrets.NewBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	auditLog := audit.New(database)
	h := &harness{service: registry.NewService(database, auditLog, box, clock), store: database, auditLog: auditLog}
	err = database.Write(t.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(t.Context(), `INSERT INTO users (username, password_hash, role, created_at) VALUES ('alice', 'x', 'admin', 0)`)
		if err != nil {
			return err
		}
		userID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		h.admin = auth.Session{UserID: userID, Username: "alice", Role: auth.RoleAdmin}
		result, err = tx.ExecContext(t.Context(), `INSERT INTO agents (public_id, public_key, hostname, os, arch, agent_version, protocol_version, enrolled_at)
			VALUES ('01JBX3K5Q7Z8M4N6P8R0T2V4W0', x'00', 'nas', 'linux', 'amd64', 'v1', 1, 0)`)
		if err != nil {
			return err
		}
		h.agentID, err = result.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func browser() auth.RequestInfo { return auth.RequestInfo{IP: "203.0.113.7"} }

func TestLogins(t *testing.T) {
	t.Run("SEC-42: stores a registry password encrypted, and hands it out only for its registry", func(t *testing.T) {
		h := newHarness(t)
		login, err := h.service.Add(t.Context(), h.admin, browser(), registry.LoginInput{
			ServerAddress: " GHCR.io ", Username: "acme-bot", Password: "ghp_secret-token"})
		if err != nil || login.ServerAddress != "ghcr.io" {
			t.Fatalf("login = %+v (%v)", login, err)
		}
		var stored []byte
		err = h.store.Read(t.Context(), func(tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT password FROM registry_credentials`).Scan(&stored)
		})
		if err != nil || bytes.Contains(stored, []byte("ghp_secret-token")) {
			t.Errorf("the password is stored in the clear (%v)", err)
		}
		got, err := h.service.AuthFor(t.Context(), "ghcr.io/acme/app:2.1")
		want := &protocol.RegistryAuth{ServerAddress: "ghcr.io", Username: "acme-bot", Password: "ghp_secret-token"}
		if err != nil || got == nil || *got != *want {
			t.Errorf("auth = %+v (%v)", got, err)
		}
		if none, err := h.service.AuthFor(t.Context(), "nginx:1.27"); err != nil || none != nil {
			t.Errorf("Docker Hub image got %+v (%v)", none, err)
		}
		logins, err := h.service.List(t.Context())
		if err != nil || len(logins) != 1 || logins[0].Username != "acme-bot" {
			t.Errorf("logins = %+v (%v)", logins, err)
		}
	})

	t.Run("refuses a second login for a registry, and invalid input", func(t *testing.T) {
		h := newHarness(t)
		input := registry.LoginInput{ServerAddress: "docker.io", Username: "me", Password: "pw"}
		if _, err := h.service.Add(t.Context(), h.admin, browser(), input); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.Add(t.Context(), h.admin, browser(), input); !errors.Is(err, registry.ErrExists) {
			t.Errorf("second login: %v", err)
		}
		for _, bad := range []registry.LoginInput{
			{ServerAddress: "https://ghcr.io", Username: "a", Password: "b"},
			{ServerAddress: "ghcr.io", Password: "b"},
			{ServerAddress: "ghcr.io", Username: "a"},
		} {
			var validation *auth.ValidationError
			if _, err := h.service.Add(t.Context(), h.admin, browser(), bad); !errors.As(err, &validation) {
				t.Errorf("%+v: %v", bad, err)
			}
		}
		if _, err := h.service.AuthFor(t.Context(), "Not A Reference"); err == nil {
			t.Error("an invalid reference passed")
		}
	})

	t.Run("removes a login, auditing both changes", func(t *testing.T) {
		h := newHarness(t)
		login, err := h.service.Add(t.Context(), h.admin, browser(), registry.LoginInput{ServerAddress: "docker.io", Username: "me", Password: "pw"})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.service.Remove(t.Context(), h.admin, browser(), login.ID); err != nil {
			t.Fatal(err)
		}
		if err := h.service.Remove(t.Context(), h.admin, browser(), login.ID); !errors.Is(err, registry.ErrNotFound) {
			t.Errorf("second remove: %v", err)
		}
		records, err := h.auditLog.List(t.Context(), audit.ListOptions{Limit: 10})
		if err != nil || len(records) != 2 || records[0].Action != "registry.remove" || records[1].Action != "registry.add" {
			t.Errorf("audit = %+v (%v)", records, err)
		}
		if bytes.Contains(records[1].Details, []byte("pw")) {
			t.Error("the audit entry holds the password")
		}
	})
}

// fakeAgents answers image list and check requests like an agent.
type fakeAgents struct {
	checked []protocol.ImageRefPayload
}

func (f *fakeAgents) Request(_ context.Context, _ int64, action string, payload, result any) error {
	switch action {
	case protocol.ActionDockerImageList:
		*result.(*protocol.ImageListPayload) = protocol.ImageListPayload{Images: []protocol.Image{ //nolint:forcetypeassert // the test's own request
			{Tags: []string{"nginx:1.27", "nginx:latest"}}, {Tags: []string{}}, {Tags: []string{"ghcr.io/acme/app:2.1"}},
		}}
		return nil
	case protocol.ActionDockerImageCheck:
		request := payload.(protocol.ImageRefPayload) //nolint:forcetypeassert // as above
		f.checked = append(f.checked, request)
		if request.Reference == "nginx:latest" {
			return &protocol.Error{Code: protocol.ErrorInternal, Message: "429 Too Many Requests"}
		}
		*result.(*protocol.ImageCheckPayload) = protocol.ImageCheckPayload{RegistryDigest: "sha256:new", UpdateAvailable: request.Reference == "nginx:1.27"} //nolint:forcetypeassert // as above
		return nil
	default:
		return errors.New("unexpected " + action)
	}
}

func TestCheckMachine(t *testing.T) {
	t.Run("checks each tag, with its registry's login, and keeps the results", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.Add(t.Context(), h.admin, browser(), registry.LoginInput{ServerAddress: "ghcr.io", Username: "bot", Password: "pw"}); err != nil {
			t.Fatal(err)
		}
		agents := &fakeAgents{}
		checked, err := h.service.CheckMachine(t.Context(), agents, h.agentID)
		if err != nil || len(checked) != 3 {
			t.Fatalf("checked = %+v (%v)", checked, err)
		}
		if agents.checked[2].Auth == nil || agents.checked[2].Auth.Password != "pw" || agents.checked[0].Auth != nil {
			t.Errorf("logins sent = %+v", agents.checked)
		}
		updates, err := h.service.Updates(t.Context(), h.agentID)
		if err != nil {
			t.Fatal(err)
		}
		if !updates["nginx:1.27"].UpdateAvailable || updates["ghcr.io/acme/app:2.1"].UpdateAvailable {
			t.Errorf("updates = %+v", updates)
		}
		if failed := updates["nginx:latest"]; failed.Error == "" || failed.UpdateAvailable {
			t.Errorf("a failed check = %+v", failed)
		}
		// Checking again replaces the old results.
		if _, err := h.service.CheckMachine(t.Context(), agents, h.agentID); err != nil {
			t.Fatal(err)
		}
		if again, _ := h.service.Updates(t.Context(), h.agentID); len(again) != 3 {
			t.Errorf("updates after a second check = %d", len(again))
		}
	})

	t.Run("reports a machine that can't list its images", func(t *testing.T) {
		h := newHarness(t)
		failing := requesterFunc(func(context.Context, int64, string, any, any) error { return errors.New("offline") })
		if _, err := h.service.CheckMachine(t.Context(), failing, h.agentID); err == nil {
			t.Error("no error")
		}
	})
}

type requesterFunc func(ctx context.Context, agentID int64, action string, payload, result any) error

func (f requesterFunc) Request(ctx context.Context, agentID int64, action string, payload, result any) error {
	return f(ctx, agentID, action, payload, result)
}

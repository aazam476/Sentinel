// SPDX-License-Identifier: Apache-2.0

package registry_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/registry"
)

var errBroken = errors.New("broken")

// brokenDatabase fails every transaction.
type brokenDatabase struct{}

func (brokenDatabase) Read(context.Context, func(*sql.Tx) error) error  { return errBroken }
func (brokenDatabase) Write(context.Context, func(*sql.Tx) error) error { return errBroken }

// brokenSealer fails to encrypt and decrypt.
type brokenSealer struct{}

func (brokenSealer) Encrypt([]byte, []byte) ([]byte, error) { return nil, errBroken }
func (brokenSealer) Decrypt([]byte, []byte) ([]byte, error) { return nil, errBroken }

// brokenAudit fails to record.
type brokenAudit struct{}

func (brokenAudit) RecordInTx(context.Context, *sql.Tx, audit.Entry) error { return errBroken }

// readOnlyDatabase reads from a real database but fails every write.
type readOnlyDatabase struct{ real registry.Database }

func (d readOnlyDatabase) Read(ctx context.Context, work func(*sql.Tx) error) error {
	return d.real.Read(ctx, work)
}

func (readOnlyDatabase) Write(context.Context, func(*sql.Tx) error) error { return errBroken }

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(0, 0) }

func TestFailures(t *testing.T) {
	login := registry.LoginInput{ServerAddress: "ghcr.io", Username: "bot", Password: "pw"}

	t.Run("passes on a database that fails", func(t *testing.T) {
		h := newHarness(t)
		service := registry.NewService(brokenDatabase{}, h.auditLog, brokenSealer{}, fixedClock{})
		if _, err := service.List(t.Context()); !errors.Is(err, errBroken) {
			t.Errorf("List: %v", err)
		}
		if _, err := service.Updates(t.Context(), 1); !errors.Is(err, errBroken) {
			t.Errorf("Updates: %v", err)
		}
		if err := service.Remove(t.Context(), h.admin, browser(), 1); !errors.Is(err, errBroken) {
			t.Errorf("Remove: %v", err)
		}
		if _, err := service.AuthFor(t.Context(), "nginx"); !errors.Is(err, errBroken) {
			t.Errorf("AuthFor: %v", err)
		}
		if _, err := service.CheckMachine(t.Context(), &fakeAgents{}, 1); !errors.Is(err, errBroken) {
			t.Errorf("CheckMachine: %v", err)
		}
	})

	t.Run("stops checking when results can't be stored", func(t *testing.T) {
		h := newHarness(t)
		service := registry.NewService(readOnlyDatabase{h.store}, h.auditLog, brokenSealer{}, fixedClock{})
		if _, err := service.CheckMachine(t.Context(), &fakeAgents{}, h.agentID); !errors.Is(err, errBroken) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("stores nothing when encryption or the audit log fails", func(t *testing.T) {
		h := newHarness(t)
		noSealer := registry.NewService(h.store, h.auditLog, brokenSealer{}, fixedClock{})
		if _, err := noSealer.Add(t.Context(), h.admin, browser(), login); !errors.Is(err, errBroken) {
			t.Errorf("Add with a broken sealer: %v", err)
		}
		box := h.service
		if _, err := box.Add(t.Context(), h.admin, browser(), login); err != nil {
			t.Fatal(err)
		}
		// A password sealed with another key can't be read back.
		if _, err := noSealer.AuthFor(t.Context(), "ghcr.io/acme/app"); !errors.Is(err, errBroken) {
			t.Errorf("AuthFor with a broken sealer: %v", err)
		}
		// A login whose password can't be decrypted fails its checks, which say why.
		checked, err := noSealer.CheckMachine(t.Context(), &fakeAgents{}, h.agentID)
		if err != nil || checked[2].Error == "" {
			t.Errorf("checked = %+v (%v)", checked, err)
		}
		noAudit := registry.NewService(h.store, brokenAudit{}, brokenSealer{}, fixedClock{})
		logins, _ := h.service.List(t.Context())
		if err := noAudit.Remove(t.Context(), h.admin, browser(), logins[0].ID); !errors.Is(err, errBroken) {
			t.Errorf("Remove with a broken audit log: %v", err)
		}
		if after, _ := h.service.List(t.Context()); len(after) != 1 {
			t.Errorf("the login was removed without an audit entry")
		}
	})
}

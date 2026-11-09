// SPDX-License-Identifier: Apache-2.0

package alerts_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/alerts"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/secrets"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

type harness struct {
	service  *alerts.Service
	database *store.Store
	clock    *fakeclock.Clock
	agentID  int64
	publicID string
	admin    auth.Session
	viewer   auth.Session
	online   map[int64]bool
	changed  []string
	// receiver records what is posted to it, as webhook, ntfy and Discord
	// servers.
	receiver *receiver
}

const nasID = "01JBX3K5Q7Z8M4N6P8R0T2V4W0"

func newHarness(t *testing.T) *harness {
	t.Helper()
	clock := fakeclock.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() }) // nothing to check when a test database closes
	box, err := secrets.NewBox(make([]byte, secrets.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{database: database, clock: clock, publicID: nasID, online: map[int64]bool{}, receiver: newReceiver(t)}
	var mutex sync.Mutex
	h.service = alerts.NewService(alerts.Options{
		Database: database, AuditLog: audit.New(database), Sealer: box, Clock: clock,
		Online: func(agentID int64) bool { return h.online[agentID] },
		Changed: func(_ int64, publicID string) {
			mutex.Lock()
			defer mutex.Unlock()
			h.changed = append(h.changed, publicID)
		},
	})
	h.agentID = h.addMachine(t, nasID, "nas", "storage")
	verified := clock.Now().UnixMilli()
	err = database.Write(t.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(t.Context(), `INSERT INTO users (username, password_hash, role, created_at) VALUES ('ada', 'x', 'admin', 0)`)
		if err != nil {
			return err
		}
		userID, err := result.LastInsertId()
		h.admin = auth.Session{UserID: userID, Username: "ada", Role: auth.RoleAdmin, SecondFactorAt: &verified}
		h.viewer = auth.Session{UserID: userID, Username: "ada", Role: auth.RoleViewer, SecondFactorAt: &verified}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// addMachine enrolls a machine with tags and returns its internal ID.
func (h *harness) addMachine(t *testing.T, publicID, hostname string, tags ...string) int64 {
	t.Helper()
	var id int64
	err := h.database.Write(t.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(t.Context(), `INSERT INTO agents (public_id, public_key, hostname, os, arch, agent_version, protocol_version, enrolled_at)
			VALUES (?, x'00', ?, 'linux', 'amd64', 'v1', 1, 0)`, publicID, hostname)
		if err != nil {
			return err
		}
		if id, err = result.LastInsertId(); err != nil {
			return err
		}
		for _, tag := range tags {
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO agent_tags (agent_id, tag) VALUES (?, ?)`, id, tag); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// exec runs a statement against the test database.
func (h *harness) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	err := h.database.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), query, args...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// received is one request the receiver got.
type received struct {
	Path    string
	Headers http.Header
	Body    string
}

// JSON decodes the request's body.
func (r received) JSON(t *testing.T) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
		t.Fatalf("%s isn't JSON: %v", r.Body, err)
	}
	return body
}

type receiver struct {
	url    string
	mutex  sync.Mutex
	got    []received
	status int
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{status: http.StatusOK}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		r.mutex.Lock()
		defer r.mutex.Unlock()
		r.got = append(r.got, received{Path: request.URL.Path, Headers: request.Header.Clone(), Body: string(body)})
		w.WriteHeader(r.status)
	}))
	t.Cleanup(server.Close)
	r.url = server.URL
	return r
}

func (r *receiver) take() []received {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	got := r.got
	r.got = nil
	return got
}

func (r *receiver) refuse(status int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.status = status
}

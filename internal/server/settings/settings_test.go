// SPDX-License-Identifier: Apache-2.0

package settings_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/metrics"
	"github.com/aazam476/sentinel/internal/server/settings"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

type fakeRetainer struct{ applied []metrics.Retention }

func (f *fakeRetainer) SetRetention(r metrics.Retention) { f.applied = append(f.applied, r) }

// stepUp passes or refuses every check.
type stepUp struct{ err error }

func (s stepUp) RequireRecentSecondFactor(auth.Session) error { return s.err }

type harness struct {
	service  *settings.Service
	retainer *fakeRetainer
	database *store.Store
	admin    auth.Session
}

func newHarness(t *testing.T, version string, check error) *harness {
	t.Helper()
	clock := fakeclock.New(time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	err = database.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (username, password_hash, role, created_at) VALUES ('ada', 'x', 'admin', 0)`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	signedIn := clock.Now().UnixMilli()
	admin := auth.Session{UserID: 1, Username: "ada", Role: auth.RoleAdmin, SecondFactorAt: &signedIn}
	h := &harness{retainer: &fakeRetainer{}, database: database, admin: admin}
	h.service = settings.New(settings.Options{
		Database: database, AuditLog: audit.New(database), Metrics: h.retainer, StepUp: stepUp{check}, ServerVersion: version,
	})
	return h
}

func TestDefaultsFollowTheServersOwnRelease(t *testing.T) {
	for version, want := range map[string]string{"v1.0.0": "stable", "v0.1.0-beta.1": "beta", "dev": "beta", "": "beta"} {
		h := newHarness(t, version, nil)
		got, err := h.service.Get(t.Context())
		if err != nil || got.UpdateChannel != want || got.Retention != (settings.Retention{RawHours: 24, MinuteDays: 14, HourDays: 365}) {
			t.Errorf("%q: settings = %+v, %v; want the §9.1 retention and %s", version, got, err, want)
		}
		if h.service.Channel(t.Context()) != want {
			t.Errorf("%q: Channel = %q", version, h.service.Channel(t.Context()))
		}
	}
}

func TestUpdateSavesAppliesAndAudits(t *testing.T) {
	h := newHarness(t, "v1.0.0", nil)
	next := settings.Settings{Retention: settings.Retention{RawHours: 48, MinuteDays: 30, HourDays: 730}, UpdateChannel: "beta"}
	if _, err := h.service.Update(t.Context(), h.admin, next, auth.RequestInfo{IP: "192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.service.Get(t.Context()); got != next {
		t.Errorf("stored = %+v", got)
	}
	want := metrics.Retention{Raw: 48 * time.Hour, Minute: 30 * 24 * time.Hour, Hour: 730 * 24 * time.Hour}
	if len(h.retainer.applied) != 1 || h.retainer.applied[0] != want {
		t.Errorf("applied = %+v", h.retainer.applied)
	}
	var action string
	if err := h.database.Read(t.Context(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT action FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&action)
	}); err != nil || action != "settings.change" {
		t.Errorf("audit = %q, %v", action, err)
	}

	restarted := settings.New(settings.Options{Database: h.database, AuditLog: audit.New(h.database), Metrics: h.retainer, ServerVersion: "v1.0.0"})
	if err := restarted.Apply(t.Context()); err != nil || h.retainer.applied[1] != want {
		t.Errorf("Apply after a restart = %v, applied %+v", err, h.retainer.applied)
	}
}

func TestUpdateRefuses(t *testing.T) {
	good := settings.Retention{RawHours: 24, MinuteDays: 14, HourDays: 365}
	tests := []struct {
		name      string
		settings  settings.Settings
		check     error
		role      auth.Role
		wantField string
		wantErr   error
	}{
		{name: "a viewer", settings: settings.Settings{Retention: good, UpdateChannel: "stable"}, role: auth.RoleViewer, wantErr: auth.ErrForbidden},
		{name: "an admin without a recent check", settings: settings.Settings{Retention: good, UpdateChannel: "stable"}, check: auth.ErrStepUpRequired, wantErr: auth.ErrStepUpRequired},
		{name: "an unknown channel", settings: settings.Settings{Retention: good, UpdateChannel: "nightly"}, wantField: "updateChannel"},
		{name: "no raw samples", settings: settings.Settings{Retention: settings.Retention{RawHours: 0, MinuteDays: 14, HourDays: 365}, UpdateChannel: "stable"}, wantField: "rawHours"},
		{name: "a quarter of minute averages", settings: settings.Settings{Retention: settings.Retention{RawHours: 24, MinuteDays: 91, HourDays: 365}, UpdateChannel: "stable"}, wantField: "minuteDays"},
		{name: "ten years of hours", settings: settings.Settings{Retention: settings.Retention{RawHours: 24, MinuteDays: 14, HourDays: 3650}, UpdateChannel: "stable"}, wantField: "hourDays"},
		{name: "raw samples outliving minute averages", settings: settings.Settings{Retention: settings.Retention{RawHours: 72, MinuteDays: 2, HourDays: 365}, UpdateChannel: "stable"}, wantField: "retention"},
	}
	for _, test := range tests {
		t.Run("refuses "+test.name, func(t *testing.T) {
			h := newHarness(t, "v1.0.0", test.check)
			actor := h.admin
			if test.role != "" {
				actor.Role = test.role
			}
			_, err := h.service.Update(t.Context(), actor, test.settings, auth.RequestInfo{})
			var invalid *settings.InvalidError
			switch {
			case test.wantErr != nil && !errors.Is(err, test.wantErr):
				t.Errorf("err = %v, want %v", err, test.wantErr)
			case test.wantField != "" && (!errors.As(err, &invalid) || invalid.Field != test.wantField):
				t.Errorf("err = %v, want an invalid %s", err, test.wantField)
			}
			if len(h.retainer.applied) != 0 {
				t.Error("a refused change was applied")
			}
		})
	}
}

func TestSettingsReportDatabaseFailures(t *testing.T) {
	h := newHarness(t, "v1.0.0", nil)
	if err := h.database.Write(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO settings (key, value_json) VALUES ('metrics.retention', 'not json')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Get(t.Context()); err == nil {
		t.Error("Get read a broken setting")
	}
	if err := h.service.Apply(t.Context()); err == nil {
		t.Error("Apply applied a broken setting")
	}
	if got := h.service.Channel(t.Context()); got != "stable" {
		t.Errorf("Channel with a broken setting = %q, want the default", got)
	}
	if err := h.database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Update(t.Context(), h.admin, settings.Settings{Retention: settings.Retention{RawHours: 24, MinuteDays: 14, HourDays: 365}, UpdateChannel: "beta"}, auth.RequestInfo{}); err == nil {
		t.Error("Update on a closed database succeeded")
	}
}

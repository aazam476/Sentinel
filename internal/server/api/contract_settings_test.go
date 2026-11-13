// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/auth"
)

func TestSettingsOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()

	t.Run("shows the defaults and what server.toml sets", func(t *testing.T) {
		got := admin.browser.do(http.MethodGet, "/api/settings", nil)
		expect(t, got, http.StatusOK, "")
		var current api.SettingsResponse
		got.decode(t, &current)
		if current.Retention != (api.MetricsRetention{RawHours: 24, MinuteDays: 14, HourDays: 365}) || current.UpdateChannel == "" {
			t.Errorf("settings = %+v", current)
		}
		if current.Server.TLSMode == "" || current.Server.TrustedProxies == nil {
			t.Errorf("server config = %+v", current.Server)
		}
	})

	t.Run("saves a change and refuses a bad one", func(t *testing.T) {
		change := api.SettingsRequest{Retention: api.MetricsRetention{RawHours: 48, MinuteDays: 30, HourDays: 730}, UpdateChannel: "stable"}
		got := admin.browser.do(http.MethodPut, "/api/settings", change)
		expect(t, got, http.StatusOK, "")
		var saved api.SettingsResponse
		admin.browser.do(http.MethodGet, "/api/settings", nil).decode(t, &saved)
		if saved.Retention != change.Retention || saved.UpdateChannel != "stable" {
			t.Errorf("saved = %+v", saved)
		}
		change.UpdateChannel = "nightly"
		bad := admin.browser.do(http.MethodPut, "/api/settings", change)
		expect(t, bad, http.StatusBadRequest, "invalid_input")
		var body api.ErrorResponse
		bad.decode(t, &body)
		if body.Error.Field != "updateChannel" {
			t.Errorf("field = %q", body.Error.Field)
		}
	})

	t.Run("SEC-07 needs a recent second factor, and an admin", func(t *testing.T) {
		viewer := server.addUser(admin, "vic", "viewer")
		expect(t, viewer.do(http.MethodGet, "/api/settings", nil), http.StatusForbidden, "forbidden")
		server.clock.Advance(auth.StepUpWindow + time.Minute)
		change := api.SettingsRequest{Retention: api.MetricsRetention{RawHours: 24, MinuteDays: 14, HourDays: 365}, UpdateChannel: "beta"}
		expect(t, admin.browser.do(http.MethodPut, "/api/settings", change), http.StatusForbidden, "step_up_required")
	})
}

// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/server/api"
)

func TestUsersOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	viewer := server.addUser(admin, "vic", "viewer")
	operator := server.addUser(admin, "otto", "operator")

	t.Run("admins list users", func(t *testing.T) {
		got := admin.browser.do(http.MethodGet, "/api/users", nil)
		expect(t, got, http.StatusOK, "")
		var users api.UsersResponse
		got.decode(t, &users)
		if len(users.Users) != 3 {
			t.Errorf("users = %+v, want admin, otto and vic", users.Users)
		}
	})

	for _, roleBrowser := range []struct {
		role    string
		browser *browser
	}{{"viewer", viewer}, {"operator", operator}} {
		t.Run(roleBrowser.role+"s can't list or change users", func(t *testing.T) {
			expect(t, roleBrowser.browser.do(http.MethodGet, "/api/users", nil), http.StatusForbidden, "forbidden")
			expect(t, roleBrowser.browser.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: "x", Password: strongPassword, Role: "admin"}), http.StatusForbidden, "forbidden")
			expect(t, roleBrowser.browser.do(http.MethodPatch, "/api/users/1", api.UpdateUserRequest{}), http.StatusForbidden, "forbidden")
		})
	}

	t.Run("refuses a taken username", func(t *testing.T) {
		got := admin.browser.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: "VIC", Password: strongPassword, Role: "viewer"})
		expect(t, got, http.StatusConflict, "username_taken")
	})

	t.Run("refuses an unknown role", func(t *testing.T) {
		got := admin.browser.do(http.MethodPost, "/api/users", api.CreateUserRequest{Username: "zed", Password: strongPassword, Role: "god"})
		expect(t, got, http.StatusBadRequest, "invalid_input")
	})

	t.Run("changes a role", func(t *testing.T) {
		operatorRole := "operator"
		got := admin.browser.do(http.MethodPatch, "/api/users/"+userID(t, admin, "vic"), api.UpdateUserRequest{Role: &operatorRole})
		expect(t, got, http.StatusOK, "")
		var user api.UserResponse
		got.decode(t, &user)
		if user.Role != "operator" {
			t.Errorf("role = %q, want operator", user.Role)
		}
	})

	t.Run("disabling a user signs them out", func(t *testing.T) {
		disabled := true
		expect(t, admin.browser.do(http.MethodPatch, "/api/users/"+userID(t, admin, "otto"), api.UpdateUserRequest{Disabled: &disabled}), http.StatusOK, "")
		expect(t, operator.do(http.MethodGet, "/api/auth/session", nil), http.StatusUnauthorized, "not_authenticated")
	})

	t.Run("refuses to leave no admin", func(t *testing.T) {
		viewerRole := "viewer"
		expect(t, admin.browser.do(http.MethodPatch, "/api/users/"+userID(t, admin, "admin"), api.UpdateUserRequest{Role: &viewerRole}), http.StatusConflict, "last_admin")
	})

	t.Run("returns not found for an unknown user", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodPatch, "/api/users/999", api.UpdateUserRequest{}), http.StatusNotFound, "not_found")
		expect(t, admin.browser.do(http.MethodPatch, "/api/users/zero", api.UpdateUserRequest{}), http.StatusNotFound, "not_found")
	})
}

func TestSEC41_AuditLogOverHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	admin := server.setUpAdmin()
	viewer := server.addUser(admin, "vic", "viewer")

	t.Run("SEC-40 admins see entries newest first", func(t *testing.T) {
		got := admin.browser.do(http.MethodGet, "/api/audit?limit=2", nil)
		expect(t, got, http.StatusOK, "")
		var page api.AuditEntriesResponse
		got.decode(t, &page)
		if len(page.Entries) != 2 || page.Entries[0].ID <= page.Entries[1].ID || page.NextBefore == nil {
			t.Fatalf("page = %+v, want 2 entries newest first and a next cursor", page)
		}
		next := admin.browser.do(http.MethodGet, "/api/audit?limit=2&before="+strconv.FormatInt(*page.NextBefore, 10), nil)
		var nextPage api.AuditEntriesResponse
		next.decode(t, &nextPage)
		if len(nextPage.Entries) == 0 || nextPage.Entries[0].ID >= page.Entries[1].ID {
			t.Errorf("next page = %+v, want older entries", nextPage)
		}
	})

	t.Run("only admins can read the audit log", func(t *testing.T) {
		expect(t, viewer.do(http.MethodGet, "/api/audit", nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodGet, "/api/audit/export?format=csv", nil), http.StatusForbidden, "forbidden")
	})

	t.Run("refuses bad paging values", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodGet, "/api/audit?limit=0", nil), http.StatusBadRequest, "invalid_input")
		expect(t, admin.browser.do(http.MethodGet, "/api/audit?limit=999", nil), http.StatusBadRequest, "invalid_input")
		expect(t, admin.browser.do(http.MethodGet, "/api/audit?before=-1", nil), http.StatusBadRequest, "invalid_input")
	})

	t.Run("verifies the chain", func(t *testing.T) {
		var result api.AuditVerifyResponse
		got := admin.browser.do(http.MethodGet, "/api/audit/verify", nil)
		got.decode(t, &result)
		if !result.Valid || result.Checked == 0 {
			t.Errorf("verify = %+v, want valid", result)
		}
	})

	t.Run("exports CSV as a download and audits the export", func(t *testing.T) {
		got := admin.browser.do(http.MethodGet, "/api/audit/export?format=csv", nil)
		expect(t, got, http.StatusOK, "")
		if !strings.HasPrefix(got.header.Get("Content-Disposition"), `attachment; filename="sentinel-audit-2026-11-07`) {
			t.Errorf("Content-Disposition = %q", got.header.Get("Content-Disposition"))
		}
		rows, err := csv.NewReader(bytes.NewReader(got.body)).ReadAll()
		if err != nil || len(rows) < 2 || rows[len(rows)-1][5] != "audit.export" {
			t.Errorf("CSV rows = %v, %v; want the export itself as the last entry", rows, err)
		}
	})

	t.Run("exports JSON", func(t *testing.T) {
		got := admin.browser.do(http.MethodGet, "/api/audit/export?format=json", nil)
		var entries []api.AuditEntry
		got.decode(t, &entries)
		if len(entries) == 0 || got.header.Get("Content-Type") != "application/json" {
			t.Errorf("export = %d entries, %q", len(entries), got.header.Get("Content-Type"))
		}
	})

	t.Run("refuses an unknown export format", func(t *testing.T) {
		expect(t, admin.browser.do(http.MethodGet, "/api/audit/export?format=xml", nil), http.StatusBadRequest, "invalid_input")
	})
}

// userID finds a user's ID through the API.
func userID(t *testing.T, admin admin, username string) string {
	t.Helper()
	var users api.UsersResponse
	admin.browser.do(http.MethodGet, "/api/users", nil).decode(t, &users)
	for _, user := range users.Users {
		if user.Username == username {
			return strconv.FormatInt(user.ID, 10)
		}
	}
	t.Fatalf("no user %s", username)
	return ""
}

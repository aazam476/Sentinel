// SPDX-License-Identifier: Apache-2.0

package fleet_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

func TestListAgents(t *testing.T) {
	t.Run("lists machines by name, case-insensitively, for every role", func(t *testing.T) {
		h := newHarness(t)
		h.enroll(t, "web-02")
		h.enroll(t, "Backup")
		renamed := h.enroll(t, "zz-host")
		name := "apollo"
		if _, err := h.service.UpdateAgent(t.Context(), h.admin, renamed.PublicID, fleet.UpdateAgentInput{DisplayName: &name}, browser()); err != nil {
			t.Fatal(err)
		}

		for _, session := range []auth.Session{h.viewer, h.operator, h.admin} {
			agents, err := h.service.ListAgents(t.Context(), session)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(agents))
			for _, agent := range agents {
				names = append(names, agent.Name())
			}
			if want := []string{"apollo", "Backup", "web-02"}; !slices.Equal(names, want) {
				t.Errorf("%s sees %v, want %v", session.Role, names, want)
			}
		}
	})

	t.Run("leaves revoked machines out", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if _, err := h.service.RevokeAgent(t.Context(), h.admin, agent.PublicID, browser()); err != nil {
			t.Fatal(err)
		}
		agents, err := h.service.ListAgents(t.Context(), h.viewer)
		if err != nil || len(agents) != 0 {
			t.Errorf("agents = %v (%v), want none", agents, err)
		}
	})

	t.Run("needs a signed-in user", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.ListAgents(t.Context(), auth.Session{}); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("err = %v, want ErrNotAuthenticated", err)
		}
	})
}

func TestGetAgent(t *testing.T) {
	t.Run("returns a machine by its public ID", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		got, err := h.service.GetAgent(t.Context(), h.viewer, agent.PublicID)
		if err != nil || got.ID != agent.ID {
			t.Errorf("got %+v (%v)", got, err)
		}
	})

	t.Run("reports unknown and revoked machines as not found", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if _, err := h.service.RevokeAgent(t.Context(), h.admin, agent.PublicID, browser()); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{agent.PublicID, "01JBX3K5Q7Z8M4N6P8R0T2V4W6"} {
			if _, err := h.service.GetAgent(t.Context(), h.viewer, id); !errors.Is(err, auth.ErrNotFound) {
				t.Errorf("%s: err = %v, want ErrNotFound", id, err)
			}
		}
	})

	t.Run("needs a signed-in user", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.GetAgent(t.Context(), auth.Session{}, "x"); !errors.Is(err, auth.ErrNotAuthenticated) {
			t.Errorf("err = %v, want ErrNotAuthenticated", err)
		}
	})
}

func TestUpdateAgent(t *testing.T) {
	t.Run("renames and re-tags a machine, cleaning the tags, and audits it", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		name, tags := "  Living room NAS ", []string{"HomeLab", "storage", "homelab", " zfs "}
		updated, err := h.service.UpdateAgent(t.Context(), h.admin, agent.PublicID,
			fleet.UpdateAgentInput{DisplayName: &name, Tags: &tags}, browser())
		if err != nil {
			t.Fatal(err)
		}
		if updated.DisplayName != "Living room NAS" || !slices.Equal(updated.Tags, []string{"homelab", "storage", "zfs"}) {
			t.Errorf("updated = %q %v", updated.DisplayName, updated.Tags)
		}
		entry := h.lastAudit(t)
		if entry.Action != "agent.update" || entry.AgentID == nil || !strings.Contains(string(entry.Details), "Living room NAS") {
			t.Errorf("audit entry = %+v", entry)
		}
	})

	t.Run("an empty display name goes back to the hostname, and nil fields are kept", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		name, tags := "NAS", []string{"office"}
		if _, err := h.service.UpdateAgent(t.Context(), h.admin, agent.PublicID, fleet.UpdateAgentInput{DisplayName: &name, Tags: &tags}, browser()); err != nil {
			t.Fatal(err)
		}
		empty := ""
		updated, err := h.service.UpdateAgent(t.Context(), h.admin, agent.PublicID, fleet.UpdateAgentInput{DisplayName: &empty}, browser())
		if err != nil {
			t.Fatal(err)
		}
		if updated.Name() != "nas-01" || !slices.Equal(updated.Tags, []string{"office"}) {
			t.Errorf("name %q, tags %v", updated.Name(), updated.Tags)
		}
	})

	tooMany := make([]string, 21)
	for i := range tooMany {
		tooMany[i] = "tag" + string(rune('a'+i))
	}
	tests := []struct {
		name  string
		input fleet.UpdateAgentInput
		field string
	}{
		{"refuses a display name over 64 characters", fleet.UpdateAgentInput{DisplayName: ptr(strings.Repeat("n", 65))}, "displayName"},
		{"refuses a display name with a bidi override", fleet.UpdateAgentInput{DisplayName: ptr("nas\u202e")}, "displayName"},
		{"refuses a tag with spaces", fleet.UpdateAgentInput{Tags: &[]string{"home lab"}}, "tags"},
		{"refuses a tag starting with a dash", fleet.UpdateAgentInput{Tags: &[]string{"-x"}}, "tags"},
		{"refuses an empty tag", fleet.UpdateAgentInput{Tags: &[]string{""}}, "tags"},
		{"refuses a tag over 32 characters", fleet.UpdateAgentInput{Tags: &[]string{strings.Repeat("t", 33)}}, "tags"},
		{"refuses more than 20 tags", fleet.UpdateAgentInput{Tags: &tooMany}, "tags"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			agent := h.enroll(t, "nas-01")
			_, err := h.service.UpdateAgent(t.Context(), h.admin, agent.PublicID, test.input, browser())
			var validation *auth.ValidationError
			if !errors.As(err, &validation) || validation.Field != test.field {
				t.Errorf("err = %v, want a validation error for %s", err, test.field)
			}
		})
	}

	t.Run("accepts tags in other scripts", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		tags := []string{"büro", "東京", "a.b_c-d"}
		updated, err := h.service.UpdateAgent(t.Context(), h.admin, agent.PublicID, fleet.UpdateAgentInput{Tags: &tags}, browser())
		if err != nil || len(updated.Tags) != 3 {
			t.Errorf("tags = %v (%v)", updated.Tags, err)
		}
	})

	t.Run("only admins can change machines", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if _, err := h.service.UpdateAgent(t.Context(), h.operator, agent.PublicID, fleet.UpdateAgentInput{}, browser()); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("err = %v, want ErrForbidden", err)
		}
	})

	t.Run("reports a machine that doesn't exist", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.UpdateAgent(t.Context(), h.admin, "missing", fleet.UpdateAgentInput{}, browser()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestRevokeAgent(t *testing.T) {
	t.Run("revokes a machine and audits it", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		revoked, err := h.service.RevokeAgent(t.Context(), h.admin, agent.PublicID, browser())
		if err != nil {
			t.Fatal(err)
		}
		if revoked.RevokedAt == nil || revoked.ID != agent.ID {
			t.Errorf("revoked = %+v", revoked)
		}
		if entry := h.lastAudit(t); entry.Action != "agent.revoke" || entry.AgentID == nil || *entry.AgentID != agent.ID {
			t.Errorf("audit entry = %+v", entry)
		}
	})

	t.Run("can't revoke the same machine twice", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if _, err := h.service.RevokeAgent(t.Context(), h.admin, agent.PublicID, browser()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.RevokeAgent(t.Context(), h.admin, agent.PublicID, browser()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("only admins can revoke machines", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if _, err := h.service.RevokeAgent(t.Context(), h.operator, agent.PublicID, browser()); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("err = %v, want ErrForbidden", err)
		}
	})
}

func TestSetMaintenance(t *testing.T) {
	t.Run("puts a machine in maintenance and takes it out, auditing both", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		until := h.clock.Now().Add(2 * time.Hour).UnixMilli()
		updated, err := h.service.SetMaintenance(t.Context(), h.admin, agent.PublicID, &until, browser())
		if err != nil || updated.MaintenanceUntil == nil || *updated.MaintenanceUntil != until {
			t.Fatalf("got %+v, %v", updated.MaintenanceUntil, err)
		}
		if stored, _ := h.service.GetAgent(t.Context(), h.viewer, agent.PublicID); stored.MaintenanceUntil == nil || *stored.MaintenanceUntil != until {
			t.Errorf("stored %v", stored.MaintenanceUntil)
		}
		if entry := h.lastAudit(t); entry.Action != "agent.maintenance.start" || entry.AgentID == nil || *entry.AgentID != agent.ID {
			t.Errorf("audit entry = %+v", entry)
		}
		updated, err = h.service.SetMaintenance(t.Context(), h.admin, agent.PublicID, nil, browser())
		if err != nil || updated.MaintenanceUntil != nil {
			t.Fatalf("got %+v, %v", updated.MaintenanceUntil, err)
		}
		if entry := h.lastAudit(t); entry.Action != "agent.maintenance.end" {
			t.Errorf("audit entry = %+v", entry)
		}
	})

	for _, tc := range []struct {
		name  string
		after time.Duration
	}{
		{"refuses a time that has passed", -time.Minute},
		{"refuses a time more than 30 days away", 31 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			agent := h.enroll(t, "nas-01")
			until := h.clock.Now().Add(tc.after).UnixMilli()
			_, err := h.service.SetMaintenance(t.Context(), h.admin, agent.PublicID, &until, browser())
			var invalid *auth.ValidationError
			if !errors.As(err, &invalid) || invalid.Field != "until" {
				t.Errorf("err = %v", err)
			}
		})
	}

	t.Run("only admins can, and only for a machine that exists", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if _, err := h.service.SetMaintenance(t.Context(), h.operator, agent.PublicID, nil, browser()); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("err = %v, want ErrForbidden", err)
		}
		if _, err := h.service.SetMaintenance(t.Context(), h.admin, "01JBX3K5Q7Z8M4N6P8R0T2V4W6", nil, browser()); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func ptr[T any](value T) *T {
	return &value
}

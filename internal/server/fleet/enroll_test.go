// SPDX-License-Identifier: Apache-2.0

package fleet_test

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

func TestEnroll(t *testing.T) {
	t.Run("stores the new agent with its key and a fresh ID, and audits it", func(t *testing.T) {
		h := newHarness(t)
		input := enrollInput(t, h.newToken(t).Token)
		agent, err := h.service.Enroll(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		if !protocol.IsID(agent.PublicID) || !bytes.Equal(agent.PublicKey, input.PublicKey) {
			t.Errorf("agent = %+v, want a ULID and the agent's key", agent)
		}
		if agent.Hostname != "nas-01" || agent.OS != "linux" || agent.Arch != "amd64" || agent.AgentVersion != "v0.1.0" {
			t.Errorf("agent = %+v", agent)
		}
		entry := h.lastAudit(t)
		if entry.Action != "agent.enroll" || entry.Outcome != "success" || entry.AgentID == nil || *entry.AgentID != agent.ID ||
			entry.IP != "198.51.100.20" || !strings.Contains(string(entry.Details), agent.PublicID) {
			t.Errorf("audit entry = %+v", entry)
		}
	})

	t.Run("a single-use token enrolls one agent only", func(t *testing.T) {
		h := newHarness(t)
		token := h.newToken(t).Token
		if _, err := h.service.Enroll(t.Context(), enrollInput(t, token)); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.Enroll(t.Context(), enrollInput(t, token)); !errors.Is(err, fleet.ErrEnrollmentRefused) {
			t.Errorf("second Enroll err = %v, want ErrEnrollmentRefused", err)
		}
		if got := string(h.lastAudit(t).Details); !strings.Contains(got, "token used up") {
			t.Errorf("audit details = %s, want the reason", got)
		}
	})

	t.Run("a token with several uses enrolls that many agents", func(t *testing.T) {
		h := newHarness(t)
		token, err := h.service.CreateEnrollmentToken(t.Context(), h.admin, fleet.CreateTokenInput{MaxUses: 3}, browser())
		if err != nil {
			t.Fatal(err)
		}
		for range 3 {
			if _, err := h.service.Enroll(t.Context(), enrollInput(t, token.Token)); err != nil {
				t.Fatal(err)
			}
		}
		tokens, err := h.service.ListEnrollmentTokens(t.Context(), h.admin)
		if err != nil || tokens[0].UseCount != 3 {
			t.Errorf("use count = %+v (%v), want 3", tokens, err)
		}
	})

	t.Run("refuses an expired token", func(t *testing.T) {
		h := newHarness(t)
		token := h.newToken(t).Token
		h.clock.Advance(fleet.DefaultTokenLifetime)
		if _, err := h.service.Enroll(t.Context(), enrollInput(t, token)); !errors.Is(err, fleet.ErrEnrollmentRefused) {
			t.Errorf("err = %v, want ErrEnrollmentRefused", err)
		}
		if got := string(h.lastAudit(t).Details); !strings.Contains(got, "token expired") {
			t.Errorf("audit details = %s", got)
		}
	})

	t.Run("refuses an unknown token and audits the attempt", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.Enroll(t.Context(), enrollInput(t, "guess")); !errors.Is(err, fleet.ErrEnrollmentRefused) {
			t.Errorf("err = %v, want ErrEnrollmentRefused", err)
		}
		if got := h.auditActions(t); !slices.Equal(got, []string{"agent.enroll:failure"}) {
			t.Errorf("audit = %v", got)
		}
	})

	tests := []struct {
		name   string
		change func(*fleet.EnrollInput)
		field  string
	}{
		{"refuses a key that isn't Ed25519", func(input *fleet.EnrollInput) { input.PublicKey = []byte("short") }, "publicKey"},
		{"refuses an empty hostname", func(input *fleet.EnrollInput) { input.Hostname = "  " }, "hostname"},
		{"refuses a hostname longer than DNS allows", func(input *fleet.EnrollInput) { input.Hostname = strings.Repeat("h", 254) }, "hostname"},
		{"refuses control characters in the hostname", func(input *fleet.EnrollInput) { input.Hostname = "nas\x1b[31m" }, "hostname"},
		{"refuses an empty OS", func(input *fleet.EnrollInput) { input.OS = "" }, "os"},
		{"refuses an empty architecture", func(input *fleet.EnrollInput) { input.Arch = "" }, "arch"},
		{"refuses an empty agent version", func(input *fleet.EnrollInput) { input.AgentVersion = "" }, "agentVersion"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			token := h.newToken(t).Token
			input := enrollInput(t, token)
			test.change(&input)
			_, err := h.service.Enroll(t.Context(), input)
			var validation *auth.ValidationError
			if !errors.As(err, &validation) || validation.Field != test.field {
				t.Errorf("err = %v, want a validation error for %s", err, test.field)
			}
			// The token wasn't used up by the bad request.
			if _, err := h.service.Enroll(t.Context(), enrollInput(t, token)); err != nil {
				t.Errorf("token no longer works: %v", err)
			}
		})
	}

	t.Run("gives agents enrolled in the same millisecond different IDs", func(t *testing.T) {
		h := newHarness(t)
		first, second := h.enroll(t, "a"), h.enroll(t, "b")
		if first.PublicID == second.PublicID {
			t.Errorf("both agents got ID %s", first.PublicID)
		}
	})

	t.Run("records when the agent enrolled", func(t *testing.T) {
		h := newHarness(t)
		h.clock.Advance(time.Hour)
		agent := h.enroll(t, "nas-01")
		if agent.EnrolledAt != h.clock.Now().UnixMilli() || agent.LastSeenAt == nil {
			t.Errorf("enrolled at %d, last seen %v", agent.EnrolledAt, agent.LastSeenAt)
		}
	})
}

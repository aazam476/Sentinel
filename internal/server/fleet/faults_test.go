// SPDX-License-Identifier: Apache-2.0

package fleet_test

import (
	"errors"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// fixture is what most fault tests act on.
type fixture struct {
	agent fleet.Agent
	token fleet.CreatedToken
}

// failingReads makes every read transaction fail.
func failingReads(h *harness) func(t *testing.T) {
	return func(*testing.T) { h.database.failReads.Store(true) }
}

// failingWrites makes every write transaction fail.
func failingWrites(h *harness) func(t *testing.T) {
	return func(*testing.T) { h.database.failWrites.Store(true) }
}

// failing makes one kind of statement on one table fail.
func failing(h *harness, kind, table string) func(t *testing.T) {
	return func(t *testing.T) { h.failStatement(t, kind, table) }
}

// corrupting stores a value the service can't read back.
func corrupting(h *harness, statement string) func(t *testing.T) {
	return func(t *testing.T) { h.exec(t, statement) }
}

func TestDatabaseFailuresAreReported(t *testing.T) {
	tests := []struct {
		name  string
		fault func(h *harness) func(t *testing.T)
		run   func(t *testing.T, h *harness, f fixture) error
	}{
		{"creating a token", failingWrites, func(t *testing.T, h *harness, _ fixture) error {
			_, err := h.service.CreateEnrollmentToken(t.Context(), h.admin, fleet.CreateTokenInput{}, browser())
			return err
		}},
		{"storing a token", func(h *harness) func(t *testing.T) { return failing(h, "INSERT", "enrollment_tokens") },
			func(t *testing.T, h *harness, _ fixture) error {
				_, err := h.service.CreateEnrollmentToken(t.Context(), h.admin, fleet.CreateTokenInput{}, browser())
				return err
			}},
		{"listing tokens", failingReads, func(t *testing.T, h *harness, _ fixture) error {
			_, err := h.service.ListEnrollmentTokens(t.Context(), h.admin)
			return err
		}},
		{"revoking a token", func(h *harness) func(t *testing.T) { return failing(h, "UPDATE", "enrollment_tokens") },
			func(t *testing.T, h *harness, f fixture) error {
				return h.service.RevokeEnrollmentToken(t.Context(), h.admin, f.token.ID, browser())
			}},
		{"spending a token use", func(h *harness) func(t *testing.T) { return failing(h, "UPDATE", "enrollment_tokens") },
			func(t *testing.T, h *harness, f fixture) error {
				_, err := h.service.Enroll(t.Context(), enrollInput(t, f.token.Token))
				return err
			}},
		{"storing a new agent", func(h *harness) func(t *testing.T) { return failing(h, "INSERT", "agents") },
			func(t *testing.T, h *harness, f fixture) error {
				_, err := h.service.Enroll(t.Context(), enrollInput(t, f.token.Token))
				return err
			}},
		{"auditing a refused enrollment", func(h *harness) func(t *testing.T) { return failing(h, "INSERT", "audit_log") },
			func(t *testing.T, h *harness, _ fixture) error {
				_, err := h.service.Enroll(t.Context(), enrollInput(t, "wrong"))
				return err
			}},
		{"listing agents", failingReads, func(t *testing.T, h *harness, _ fixture) error {
			_, err := h.service.ListAgents(t.Context(), h.viewer)
			return err
		}},
		{"reading a stored agent", func(h *harness) func(t *testing.T) {
			return corrupting(h, `UPDATE agents SET system_info_json = 'not json'`)
		}, func(t *testing.T, h *harness, _ fixture) error {
			_, err := h.service.ListAgents(t.Context(), h.viewer)
			return err
		}},
		{"reading broken system info", func(h *harness) func(t *testing.T) {
			return corrupting(h, `UPDATE agents SET system_info_json = '{'`)
		}, func(t *testing.T, h *harness, f fixture) error {
			_, err := h.service.GetAgent(t.Context(), h.viewer, f.agent.PublicID)
			return err
		}},
		{"getting an agent", failingReads, func(t *testing.T, h *harness, f fixture) error {
			_, err := h.service.GetAgent(t.Context(), h.viewer, f.agent.PublicID)
			return err
		}},
		{"renaming an agent", func(h *harness) func(t *testing.T) { return failing(h, "UPDATE", "agents") },
			func(t *testing.T, h *harness, f fixture) error {
				_, err := h.service.UpdateAgent(t.Context(), h.admin, f.agent.PublicID, fleet.UpdateAgentInput{DisplayName: ptr("x")}, browser())
				return err
			}},
		{"clearing tags", func(h *harness) func(t *testing.T) {
			return func(t *testing.T) {
				h.exec(t, `INSERT INTO agent_tags (agent_id, tag) SELECT id, 'old' FROM agents`)
				h.failStatement(t, "DELETE", "agent_tags")
			}
		},
			func(t *testing.T, h *harness, f fixture) error {
				_, err := h.service.UpdateAgent(t.Context(), h.admin, f.agent.PublicID, fleet.UpdateAgentInput{Tags: &[]string{"a"}}, browser())
				return err
			}},
		{"adding tags", func(h *harness) func(t *testing.T) { return failing(h, "INSERT", "agent_tags") },
			func(t *testing.T, h *harness, f fixture) error {
				_, err := h.service.UpdateAgent(t.Context(), h.admin, f.agent.PublicID, fleet.UpdateAgentInput{Tags: &[]string{"a"}}, browser())
				return err
			}},
		{"finding an agent to update", failingWrites, func(t *testing.T, h *harness, f fixture) error {
			_, err := h.service.UpdateAgent(t.Context(), h.admin, f.agent.PublicID, fleet.UpdateAgentInput{}, browser())
			return err
		}},
		{"revoking an agent", func(h *harness) func(t *testing.T) { return failing(h, "UPDATE", "agents") },
			func(t *testing.T, h *harness, f fixture) error {
				_, err := h.service.RevokeAgent(t.Context(), h.admin, f.agent.PublicID, browser())
				return err
			}},
		{"finding an agent to revoke", failingWrites, func(t *testing.T, h *harness, f fixture) error {
			_, err := h.service.RevokeAgent(t.Context(), h.admin, f.agent.PublicID, browser())
			return err
		}},
		{"looking up an agent for a handshake", failingReads, func(t *testing.T, h *harness, f fixture) error {
			_, err := h.service.AgentForHandshake(t.Context(), f.agent.PublicID)
			return err
		}},
		{"checking revocation", failingReads, func(t *testing.T, h *harness, f fixture) error {
			_, err := h.service.IsRevoked(t.Context(), f.agent.ID)
			return err
		}},
		{"checking revocation of a missing agent", func(*harness) func(t *testing.T) { return func(*testing.T) {} },
			func(t *testing.T, h *harness, _ fixture) error {
				_, err := h.service.IsRevoked(t.Context(), 999)
				return err
			}},
		{"recording a connection", func(h *harness) func(t *testing.T) { return failing(h, "UPDATE", "agents") },
			func(t *testing.T, h *harness, f fixture) error {
				return h.service.RecordConnected(t.Context(), f.agent.ID, "v1", 1)
			}},
		{"recording last seen", func(h *harness) func(t *testing.T) { return failing(h, "UPDATE", "agents") },
			func(t *testing.T, h *harness, f fixture) error {
				return h.service.RecordSeen(t.Context(), f.agent.ID)
			}},
		{"saving system info", func(h *harness) func(t *testing.T) { return failing(h, "UPDATE", "agents") },
			func(t *testing.T, h *harness, f fixture) error {
				return h.service.SaveSystemInfo(t.Context(), f.agent.ID, protocol.SystemInfo{Hostname: "h"})
			}},
		{"clearing capabilities", func(h *harness) func(t *testing.T) {
			return func(t *testing.T) {
				h.exec(t, `INSERT INTO agent_capabilities (agent_id, capability, enabled, reason) SELECT id, 'files', 1, '' FROM agents`)
				h.failStatement(t, "DELETE", "agent_capabilities")
			}
		},
			func(t *testing.T, h *harness, f fixture) error {
				return h.service.SaveSystemInfo(t.Context(), f.agent.ID, protocol.SystemInfo{Hostname: "h"})
			}},
		{"saving capabilities", func(h *harness) func(t *testing.T) { return failing(h, "INSERT", "agent_capabilities") },
			func(t *testing.T, h *harness, f fixture) error {
				return h.service.SaveSystemInfo(t.Context(), f.agent.ID,
					protocol.SystemInfo{Hostname: "h", Capabilities: []protocol.Capability{{Name: "files"}}})
			}},
	}
	for _, test := range tests {
		t.Run(test.name+" fails cleanly", func(t *testing.T) {
			h := newHarness(t)
			f := fixture{agent: h.enroll(t, "nas-01"), token: h.newToken(t)}
			test.fault(h)(t)
			err := test.run(t, h, f)
			if err == nil || errors.Is(err, fleet.ErrEnrollmentRefused) {
				t.Errorf("err = %v, want the database failure", err)
			}
		})
	}
}

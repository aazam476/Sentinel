// SPDX-License-Identifier: Apache-2.0

package fleet_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

func TestAgentForHandshake(t *testing.T) {
	t.Run("returns the agent with its public key", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		got, err := h.service.AgentForHandshake(t.Context(), agent.PublicID)
		if err != nil || got.ID != agent.ID || !got.PublicKey.Equal(agent.PublicKey) {
			t.Errorf("got %+v (%v)", got, err)
		}
	})

	t.Run("refuses an unknown agent", func(t *testing.T) {
		h := newHarness(t)
		if _, err := h.service.AgentForHandshake(t.Context(), "01JBX3K5Q7Z8M4N6P8R0T2V4W6"); !errors.Is(err, fleet.ErrUnknownAgent) {
			t.Errorf("err = %v, want ErrUnknownAgent", err)
		}
	})

	t.Run("refuses a revoked agent", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if _, err := h.service.RevokeAgent(t.Context(), h.admin, agent.PublicID, browser()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.AgentForHandshake(t.Context(), agent.PublicID); !errors.Is(err, fleet.ErrAgentRevoked) {
			t.Errorf("err = %v, want ErrAgentRevoked", err)
		}
		if revoked, err := h.service.IsRevoked(t.Context(), agent.ID); err != nil || !revoked {
			t.Errorf("IsRevoked = %v (%v), want true", revoked, err)
		}
	})
}

func TestRecordConnected(t *testing.T) {
	t.Run("stores the agent's new version and the time", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		h.clock.Advance(time.Hour)
		if err := h.service.RecordConnected(t.Context(), agent.ID, "v0.2.0", 2); err != nil {
			t.Fatal(err)
		}
		got, err := h.service.GetAgent(t.Context(), h.viewer, agent.PublicID)
		if err != nil {
			t.Fatal(err)
		}
		if got.AgentVersion != "v0.2.0" || got.ProtocolVersion != 2 || *got.LastSeenAt != h.clock.Now().UnixMilli() {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("refuses a version with control characters", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		var validation *auth.ValidationError
		if err := h.service.RecordConnected(t.Context(), agent.ID, "v1\x1b[2J", 1); !errors.As(err, &validation) {
			t.Errorf("err = %v, want a validation error", err)
		}
	})
}

func TestRecordSeen(t *testing.T) {
	t.Run("moves last seen to now", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		h.clock.Advance(time.Minute)
		if err := h.service.RecordSeen(t.Context(), agent.ID); err != nil {
			t.Fatal(err)
		}
		got, err := h.service.GetAgent(t.Context(), h.viewer, agent.PublicID)
		if err != nil || *got.LastSeenAt != h.clock.Now().UnixMilli() {
			t.Errorf("last seen %v (%v)", got.LastSeenAt, err)
		}
	})
}

func TestSaveDesktopAvailability(t *testing.T) {
	t.Run("keeps the rest of the machine's details", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		info := protocol.SystemInfo{Hostname: "nas-01.home", CPUCores: 4, Desktop: &protocol.DesktopAvailability{Reason: "No one is logged in."}}
		if err := h.service.SaveSystemInfo(t.Context(), agent.ID, info); err != nil {
			t.Fatal(err)
		}
		if err := h.service.SaveDesktopAvailability(t.Context(), agent.ID, protocol.DesktopAvailability{Available: true}); err != nil {
			t.Fatal(err)
		}
		got, err := h.service.GetAgent(t.Context(), h.viewer, agent.PublicID)
		if err != nil || got.SystemInfo.Desktop == nil || !got.SystemInfo.Desktop.Available || got.SystemInfo.CPUCores != 4 {
			t.Errorf("got %+v (%v)", got.SystemInfo, err)
		}
	})

	t.Run("refuses a reason it won't show", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		for _, reason := range []string{"bell\x07", strings.Repeat("r", 129)} {
			if err := h.service.SaveDesktopAvailability(t.Context(), agent.ID, protocol.DesktopAvailability{Reason: reason}); err == nil {
				t.Errorf("stored %q", reason)
			}
		}
	})
}

func TestSaveSystemInfo(t *testing.T) {
	info := protocol.SystemInfo{
		Hostname: "nas-01.home", OS: "linux", Platform: "debian", PlatformVersion: "13", KernelVersion: "6.12",
		Arch: "arm64", CPUModel: "Cortex-A76", CPUCores: 4, MemoryTotal: 8 << 30, AgentVersion: "v0.1.0",
		Capabilities: []protocol.Capability{
			{Name: "terminal", Enabled: true},
			{Name: "desktop", Enabled: false, Reason: "Disabled by local policy"},
		},
	}

	t.Run("stores the machine's details and which capabilities its policy allows", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if err := h.service.SaveSystemInfo(t.Context(), agent.ID, info); err != nil {
			t.Fatal(err)
		}
		got, err := h.service.GetAgent(t.Context(), h.viewer, agent.PublicID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Hostname != "nas-01.home" || got.Arch != "arm64" || got.SystemInfo.CPUModel != "Cortex-A76" || got.SystemInfo.CPUCores != 4 {
			t.Errorf("got %+v", got)
		}
		capabilities := got.SystemInfo.Capabilities
		if len(capabilities) != 2 || capabilities[0].Name != "desktop" || capabilities[0].Enabled ||
			capabilities[0].Reason != "Disabled by local policy" || !capabilities[1].Enabled {
			t.Errorf("capabilities = %+v", capabilities)
		}
	})

	t.Run("replaces the capabilities on the next report", func(t *testing.T) {
		h := newHarness(t)
		agent := h.enroll(t, "nas-01")
		if err := h.service.SaveSystemInfo(t.Context(), agent.ID, info); err != nil {
			t.Fatal(err)
		}
		next := info
		next.Capabilities = []protocol.Capability{{Name: "docker", Enabled: true}}
		if err := h.service.SaveSystemInfo(t.Context(), agent.ID, next); err != nil {
			t.Fatal(err)
		}
		got, err := h.service.GetAgent(t.Context(), h.viewer, agent.PublicID)
		if err != nil || len(got.SystemInfo.Capabilities) != 1 || got.SystemInfo.Capabilities[0].Name != "docker" {
			t.Errorf("capabilities = %+v (%v)", got.SystemInfo.Capabilities, err)
		}
	})

	tests := []struct {
		name   string
		change func(*protocol.SystemInfo)
	}{
		{"refuses an empty hostname", func(info *protocol.SystemInfo) { info.Hostname = "" }},
		{"refuses control characters in the platform", func(info *protocol.SystemInfo) { info.Platform = "debian\x07" }},
		{"refuses a capability without a name", func(info *protocol.SystemInfo) {
			info.Capabilities = []protocol.Capability{{Name: ""}}
		}},
		{"refuses a reason that is too long", func(info *protocol.SystemInfo) {
			info.Capabilities = []protocol.Capability{{Name: "files", Reason: strings.Repeat("r", 129)}}
		}},
		{"refuses a desktop reason that is too long", func(info *protocol.SystemInfo) {
			info.Desktop = &protocol.DesktopAvailability{Reason: strings.Repeat("r", 129)}
		}},
		{"refuses an absurd number of capabilities", func(info *protocol.SystemInfo) {
			info.Capabilities = make([]protocol.Capability, 65)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			agent := h.enroll(t, "nas-01")
			bad := info
			bad.Capabilities = append([]protocol.Capability(nil), info.Capabilities...)
			test.change(&bad)
			if err := h.service.SaveSystemInfo(t.Context(), agent.ID, bad); err == nil {
				t.Error("SaveSystemInfo returned nil")
			}
		})
	}
}

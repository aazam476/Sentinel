// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
)

func TestPackages(t *testing.T) {
	path := func(f terminalFixture) string { return "/api/agents/" + f.agentID + "/packages" }

	t.Run("lists a machine's updates", func(t *testing.T) {
		f := newTerminalFixture(t)
		asked := answerNext(t, f.agent, protocol.PackageUpdatesPayload{RestartRequired: true, Sources: []protocol.PackageSource{
			{Manager: "apt", Selectable: true, Updates: []protocol.PackageUpdate{{ID: "openssl", Name: "openssl", Current: "3.0.1", Available: "3.0.2", Security: true}}},
			{Manager: "homebrew", Error: "brew: offline", Updates: []protocol.PackageUpdate{}},
		}}, nil)
		got := f.admin.browser.do(http.MethodGet, path(f), nil)
		expect(t, got, http.StatusOK, "")
		var list api.PackageUpdatesResponse
		got.decode(t, &list)
		want := api.PackageUpdate{ID: "openssl", Name: "openssl", Current: "3.0.1", Available: "3.0.2", Security: true}
		if !list.RestartRequired || len(list.Sources) != 2 || list.Sources[0].Updates[0] != want || list.Sources[1].Error != "brew: offline" {
			t.Errorf("list = %+v", list)
		}
		if request := <-asked; request.action != protocol.ActionPackageListUpdates {
			t.Errorf("agent got %+v", request)
		}
	})

	t.Run("installs chosen updates and audits it", func(t *testing.T) {
		f := newTerminalFixture(t)
		asked := answerNext(t, f.agent, protocol.PackageApplyResult{Output: "Done.", RestartRequired: true}, nil)
		got := f.admin.browser.do(http.MethodPost, path(f), api.ApplyPackagesRequest{Manager: "apt", IDs: []string{"openssl"}})
		expect(t, got, http.StatusOK, "")
		var result api.ApplyPackagesResponse
		got.decode(t, &result)
		if result != (api.ApplyPackagesResponse{Output: "Done.", RestartRequired: true}) {
			t.Errorf("result = %+v", result)
		}
		if request := <-asked; request.action != protocol.ActionPackageApplyUpdates || request.payload != `{"manager":"apt","ids":["openssl"]}` {
			t.Errorf("agent got %+v", request)
		}
		if got := auditActions(t, f.server, "package."); !slices.Equal(got, []string{"package.apply.updates:success"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("SEC-22: audits the agent's policy refusal as denied", func(t *testing.T) {
		f := newTerminalFixture(t)
		answerNext(t, f.agent, nil, &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "Package updates are disabled by this machine's local policy."})
		expect(t, f.admin.browser.do(http.MethodPost, path(f), api.ApplyPackagesRequest{Manager: "apt"}), http.StatusForbidden, "policy_denied")
		if got := auditActions(t, f.server, "package."); !slices.Equal(got, []string{"package.apply.updates:denied"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("SEC-07: installing needs a recent second factor", func(t *testing.T) {
		f := newTerminalFixture(t)
		f.server.clock.Advance(16 * time.Minute)
		expect(t, f.admin.browser.do(http.MethodPost, path(f), api.ApplyPackagesRequest{Manager: "apt"}), http.StatusForbidden, "step_up_required")
	})

	t.Run("checks the request before asking the machine", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, f.admin.browser.do(http.MethodPost, path(f), api.ApplyPackagesRequest{}), http.StatusBadRequest, "invalid_input")
		expect(t, f.admin.browser.do(http.MethodPost, path(f), api.ApplyPackagesRequest{Manager: "apt", IDs: make([]string, 1001)}), http.StatusBadRequest, "invalid_input")
		expect(t, viewer.do(http.MethodGet, path(f), nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodPost, path(f), api.ApplyPackagesRequest{Manager: "apt"}), http.StatusForbidden, "forbidden")
	})

	capabilityCases := []struct {
		name       string
		capability protocol.Capability
		status     int
		code       string
	}{
		{name: "SEC-22: refuses without asking when the local policy switches packages off",
			capability: protocol.Capability{Name: "packages", Reason: protocol.ReasonDisabledByPolicy}, status: http.StatusForbidden, code: "policy_denied"},
		{name: "says when the machine has no package manager Sentinel supports",
			capability: protocol.Capability{Name: "packages", Reason: "This machine has no package manager Sentinel supports"}, status: http.StatusNotImplemented, code: "unsupported"},
	}
	for _, c := range capabilityCases {
		t.Run(c.name, func(t *testing.T) {
			server := newTestServer(t)
			admin := server.setUpAdmin()
			events, stop := server.app.Hub.Subscribe()
			defer stop()
			token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
			agentID, _ := server.connectAgentWith(token.Secret, token.ServerFingerprint, []protocol.Capability{c.capability})
			waitForAgentEvent(t, events, agentID, hub.EventInfoUpdated)
			expect(t, admin.browser.do(http.MethodGet, "/api/agents/"+agentID+"/packages", nil), c.status, c.code)
			expect(t, admin.browser.do(http.MethodPost, "/api/agents/"+agentID+"/packages", api.ApplyPackagesRequest{Manager: "apt"}), c.status, c.code)
		})
	}
}

// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
)

func TestStorage(t *testing.T) {
	t.Run("shows viewers a machine's storage and one pool's status", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		overview := protocol.StorageOverviewPayload{
			ZFS:   protocol.ZFSPresence{Installed: true},
			Pools: []protocol.ZFSPool{{Name: "tank", Health: "ONLINE", SizeBytes: 100}},
		}
		asked := answerNext(t, f.agent, overview, nil)
		got := viewer.do(http.MethodGet, "/api/agents/"+f.agentID+"/storage", nil)
		expect(t, got, http.StatusOK, "")
		var decoded protocol.StorageOverviewPayload
		got.decode(t, &decoded)
		if request := <-asked; request.action != protocol.ActionStorageOverview || len(decoded.Pools) != 1 {
			t.Errorf("agent got %+v, answer %+v", request, decoded)
		}

		asked = answerNext(t, f.agent, protocol.ZFSPool{Name: "tank", Health: "ONLINE"}, nil)
		expect(t, viewer.do(http.MethodGet, "/api/agents/"+f.agentID+"/storage/pools/tank", nil), http.StatusOK, "")
		if request := <-asked; request.action != protocol.ActionZFSPoolStatus || request.payload != `{"pool":"tank"}` {
			t.Errorf("agent got %+v", request)
		}
	})

	t.Run("passes on the agent's refusals", func(t *testing.T) {
		f := newTerminalFixture(t)
		answerNext(t, f.agent, nil, &protocol.Error{Code: protocol.ErrorNotFound, Message: "there is no pool with that name"})
		expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/storage/pools/nope", nil), http.StatusNotFound, "not_found")
		answerNext(t, f.agent, nil, &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "The storage view is disabled."})
		expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/storage", nil), http.StatusForbidden, "policy_denied")
	})

	t.Run("lists a degraded pool's alert, and counts it in the fleet", func(t *testing.T) {
		f := newTerminalFixture(t)
		events, stop := f.server.app.Hub.Subscribe()
		defer stop()
		health := protocol.StorageHealthPayload{Pools: []protocol.PoolHealth{{Name: "tank", Health: "DEGRADED"}}}
		event := protocol.Message{Type: protocol.TypeEvent, ID: protocol.NewID(f.server.clock.Now()), Action: protocol.ActionStorageHealth}
		if err := protocol.WriteMessage(t.Context(), f.agent, event, health); err != nil {
			t.Fatal(err)
		}
		for event := range events {
			if event.Kind == hub.EventAlerts {
				break
			}
		}

		got := f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/alerts", nil)
		expect(t, got, http.StatusOK, "")
		var list api.MachineAlerts
		got.decode(t, &list)
		if len(list.Alerts) != 1 || list.Alerts[0].Message != "Pool tank is DEGRADED." || list.Alerts[0].ResolvedAt != nil {
			t.Errorf("alerts = %+v", list)
		}
		var fleet api.AgentsResponse
		f.admin.browser.do(http.MethodGet, "/api/agents", nil).decode(t, &fleet)
		if len(fleet.Agents) != 1 || fleet.Agents[0].FiringAlerts != 1 {
			t.Errorf("fleet = %+v", fleet)
		}
		var details api.AgentDetails
		f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID, nil).decode(t, &details)
		if details.FiringAlerts != 1 {
			t.Errorf("details = %+v", details)
		}

		healthy := protocol.StorageHealthPayload{Pools: []protocol.PoolHealth{{Name: "tank", Health: "ONLINE"}}}
		if err := protocol.WriteMessage(t.Context(), f.agent, event, healthy); err != nil {
			t.Fatal(err)
		}
		for event := range events {
			if event.Kind == hub.EventAlerts {
				break
			}
		}
		f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/alerts", nil).decode(t, &list)
		if len(list.Alerts) != 1 || list.Alerts[0].State != "resolved" || list.Alerts[0].ResolvedAt == nil {
			t.Errorf("alerts = %+v", list)
		}
	})

	t.Run("lists no alerts for a machine that isn't there", func(t *testing.T) {
		f := newTerminalFixture(t)
		expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/01JBX3K5Q7Z8M4N6P8R0T2V4WM/alerts", nil), http.StatusNotFound, "not_found")
	})
}

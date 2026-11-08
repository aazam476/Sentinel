// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// agentRequest is what the fake agent received.
type agentRequest struct {
	action  string
	payload string
}

// answerNext plays the agent for one request: it answers with result, or
// with refusal when that is set, and reports what it was asked.
func answerNext(t *testing.T, conn *websocket.Conn, result any, refusal *protocol.Error) <-chan agentRequest {
	t.Helper()
	received := make(chan agentRequest, 1)
	go func() {
		defer close(received)
		for {
			message, frame, err := protocol.ReadAny(t.Context(), conn)
			if err != nil {
				t.Errorf("the agent's read failed: %v", err)
				return
			}
			if frame != nil || message.Type != protocol.TypeRequest {
				continue
			}
			received <- agentRequest{action: message.Action, payload: string(message.Payload)}
			response := protocol.Message{Type: protocol.TypeResponse, ID: message.ID, Error: refusal}
			if refusal != nil {
				result = nil
			}
			if err := protocol.WriteMessage(t.Context(), conn, response, result); err != nil {
				t.Errorf("the agent's answer failed: %v", err)
			}
			return
		}
	}()
	return received
}

func TestServices(t *testing.T) {
	t.Run("lists a machine's services, for viewers too", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		asked := answerNext(t, f.agent, protocol.ServiceListPayload{Manager: "systemd", Services: []protocol.Service{
			{Name: "nginx.service", Description: "A web server", State: protocol.ServiceFailed, Startup: protocol.StartupAutomatic},
		}}, nil)
		got := viewer.do(http.MethodGet, "/api/agents/"+f.agentID+"/services", nil)
		expect(t, got, http.StatusOK, "")
		var list api.ServiceList
		got.decode(t, &list)
		want := api.MachineService{Name: "nginx.service", Description: "A web server", State: "failed", Startup: "automatic"}
		if list.Manager != "systemd" || len(list.Services) != 1 || list.Services[0] != want {
			t.Errorf("list = %+v", list)
		}
		if request := <-asked; request.action != protocol.ActionServiceList {
			t.Errorf("agent was asked for %s", request.action)
		}
	})

	t.Run("restarts a service and audits it", func(t *testing.T) {
		f := newTerminalFixture(t)
		asked := answerNext(t, f.agent, nil, nil)
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/services/getty@tty1.service/restart", nil), http.StatusNoContent, "")
		if request := <-asked; request.action != protocol.ActionServiceRestart || request.payload != `{"name":"getty@tty1.service"}` {
			t.Errorf("agent got %+v", request)
		}
		if got := auditActions(t, f.server, "service."); !slices.Equal(got, []string{"service.restart:success"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("passes on the agent's refusal and audits the failure", func(t *testing.T) {
		f := newTerminalFixture(t)
		answerNext(t, f.agent, nil, &protocol.Error{Code: protocol.ErrorNotFound, Message: "there is no service with that name"})
		got := f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/services/nope.service/stop", nil)
		expect(t, got, http.StatusNotFound, "not_found")
		if want := []string{"service.stop:failure"}; !slices.Equal(auditActions(t, f.server, "service."), want) {
			t.Errorf("audit = %v", auditActions(t, f.server, "service."))
		}
	})

	t.Run("refuses unknown actions and viewers without asking the agent", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/services/nginx.service/mask", nil), http.StatusNotFound, "not_found")
		expect(t, viewer.do(http.MethodPost, "/api/agents/"+f.agentID+"/services/nginx.service/stop", nil), http.StatusForbidden, "forbidden")
	})
}

func TestLogs(t *testing.T) {
	cases := []struct {
		name, query, action, payload string
	}{
		{"reads the system log", "", protocol.ActionLogQuery, `{}`},
		{"reads one service's log with filters", "?service=nginx.service&level=warning&since=1794135600000&limit=50",
			protocol.ActionServiceLogs, `{"service":"nginx.service","since":1794135600000,"minLevel":"warning","limit":50}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newTerminalFixture(t)
			asked := answerNext(t, f.agent, protocol.LogEntriesPayload{
				Entries: []protocol.LogEntry{{Time: 1794139199000, Level: protocol.LogError, Source: "nginx", Message: "bind() failed"}}, Truncated: true}, nil)
			got := f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/logs"+c.query, nil)
			expect(t, got, http.StatusOK, "")
			var entries api.LogEntries
			got.decode(t, &entries)
			want := api.MachineLogEntry{Time: 1794139199000, Level: "error", Source: "nginx", Message: "bind() failed"}
			if !entries.Truncated || len(entries.Entries) != 1 || entries.Entries[0] != want {
				t.Errorf("entries = %+v", entries)
			}
			if request := <-asked; request.action != c.action || request.payload != c.payload {
				t.Errorf("agent got %+v", request)
			}
		})
	}

	t.Run("refuses invalid filters", func(t *testing.T) {
		f := newTerminalFixture(t)
		for field, query := range map[string]string{"level": "level=critical", "since": "since=yesterday", "limit": "limit=5000"} {
			got := f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/logs?"+query, nil)
			expect(t, got, http.StatusBadRequest, "invalid_input")
			if !containsField(got, field) {
				t.Errorf("%s: field missing in %s", query, got.body)
			}
		}
	})
}

// containsField reports whether an invalid_input response names field.
func containsField(got response, field string) bool {
	var body api.ErrorResponse
	return json.Unmarshal(got.body, &body) == nil && body.Error.Field == field
}

func TestProcesses(t *testing.T) {
	t.Run("lists processes for operators, but not viewers", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, viewer.do(http.MethodGet, "/api/agents/"+f.agentID+"/processes", nil), http.StatusForbidden, "forbidden")
		answerNext(t, f.agent, protocol.ProcessListPayload{Processes: []protocol.Process{
			{PID: 900, Name: "postgres", User: "postgres", CPUPercent: 12.5, MemoryBytes: 1 << 30, Command: "postgres -D /data"}}}, nil)
		got := f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/processes", nil)
		expect(t, got, http.StatusOK, "")
		var list api.ProcessList
		got.decode(t, &list)
		want := api.MachineProcess{PID: 900, Name: "postgres", User: "postgres", CPUPercent: 12.5, MemoryBytes: 1 << 30, Command: "postgres -D /data"}
		if len(list.Processes) != 1 || list.Processes[0] != want {
			t.Errorf("list = %+v", list)
		}
	})

	t.Run("kills a process and audits it", func(t *testing.T) {
		f := newTerminalFixture(t)
		asked := answerNext(t, f.agent, nil, nil)
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/processes/900/kill", api.KillProcessRequest{Force: true}), http.StatusNoContent, "")
		if request := <-asked; request.action != protocol.ActionProcessKill || request.payload != `{"pid":900,"force":true}` {
			t.Errorf("agent got %+v", request)
		}
		if got := auditActions(t, f.server, "process."); !slices.Equal(got, []string{"process.kill:success"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("refuses something that isn't a PID", func(t *testing.T) {
		f := newTerminalFixture(t)
		for _, pid := range []string{"0", "-5", "abc", "99999999999"} {
			expect(t, f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/processes/"+pid+"/kill", api.KillProcessRequest{}), http.StatusBadRequest, "invalid_input")
		}
	})
}

func TestPower(t *testing.T) {
	t.Run("reboots a machine and audits it", func(t *testing.T) {
		f := newTerminalFixture(t)
		asked := answerNext(t, f.agent, nil, nil)
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/power", api.PowerRequest{Action: "reboot"}), http.StatusAccepted, "")
		if request := <-asked; request.action != protocol.ActionSystemReboot {
			t.Errorf("agent got %+v", request)
		}
		if got := auditActions(t, f.server, "system."); !slices.Equal(got, []string{"system.reboot:success"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("SEC-22: audits the agent's policy refusal as denied", func(t *testing.T) {
		f := newTerminalFixture(t)
		answerNext(t, f.agent, nil, &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "Rebooting and shutting down is disabled by this machine's local policy."})
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/power", api.PowerRequest{Action: "shutdown"}), http.StatusForbidden, "policy_denied")
		if got := auditActions(t, f.server, "system."); !slices.Equal(got, []string{"system.shutdown:denied"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("SEC-22: refuses without asking when the machine reports power switched off", func(t *testing.T) {
		server := newTestServer(t)
		admin := server.setUpAdmin()
		events, stop := server.app.Hub.Subscribe()
		defer stop()
		token := createToken(t, admin, api.CreateEnrollmentTokenRequest{})
		agentID, _ := server.connectAgentWith(token.Secret, token.ServerFingerprint, []protocol.Capability{{Name: "power", Enabled: false}})
		waitForAgentEvent(t, events, agentID, hub.EventInfoUpdated)
		expect(t, admin.browser.do(http.MethodPost, "/api/agents/"+agentID+"/power", api.PowerRequest{Action: "reboot"}), http.StatusForbidden, "policy_denied")
	})

	t.Run("refuses an unknown action and viewers", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/power", api.PowerRequest{Action: "hibernate"}), http.StatusBadRequest, "invalid_input")
		expect(t, viewer.do(http.MethodPost, "/api/agents/"+f.agentID+"/power", api.PowerRequest{Action: "reboot"}), http.StatusForbidden, "forbidden")
	})

	t.Run("reports an offline machine", func(t *testing.T) {
		f := newTerminalFixture(t)
		events, stop := f.server.app.Hub.Subscribe()
		defer stop()
		_ = f.agent.CloseNow()
		waitForAgentEvent(t, events, f.agentID, hub.EventOffline)
		expect(t, f.admin.browser.do(http.MethodPost, "/api/agents/"+f.agentID+"/power", api.PowerRequest{Action: "reboot"}), http.StatusConflict, "machine_offline")
		expect(t, f.admin.browser.do(http.MethodGet, "/api/agents/"+f.agentID+"/services", nil), http.StatusConflict, "machine_offline")
	})
}

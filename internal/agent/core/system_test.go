// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/logs"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/power"
	"github.com/aazam476/sentinel/internal/agent/processes"
	"github.com/aazam476/sentinel/internal/agent/services"
	"github.com/aazam476/sentinel/internal/agent/wol"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// fakeMachine stands in for services, logs, processes and power, failing
// with err when it is set, and recording what it was asked to do.
type fakeMachine struct {
	err  error
	done chan string
}

func newFakeMachine() *fakeMachine { return &fakeMachine{done: make(chan string, 8)} }

func (m *fakeMachine) List(context.Context) (protocol.ServiceListPayload, error) {
	return protocol.ServiceListPayload{Manager: "systemd", Services: []protocol.Service{{Name: "nginx.service", State: protocol.ServiceRunning}}}, m.err
}

func (m *fakeMachine) Do(_ context.Context, action services.Action, name string) error {
	m.done <- string(action) + " " + name
	return m.err
}

func (m *fakeMachine) Query(_ context.Context, query protocol.LogQueryPayload) (protocol.LogEntriesPayload, error) {
	m.done <- "logs " + query.Service
	return protocol.LogEntriesPayload{Entries: []protocol.LogEntry{{Message: "hello"}}}, m.err
}

type fakeProcesses struct{ *fakeMachine }

func (p fakeProcesses) List(context.Context) (protocol.ProcessListPayload, error) {
	return protocol.ProcessListPayload{Processes: []protocol.Process{{PID: 42, Name: "nginx"}}}, p.err
}

func (p fakeProcesses) Kill(_ context.Context, request protocol.ProcessKillPayload) error {
	p.done <- fmt.Sprintf("kill %d %v", request.PID, request.Force)
	return p.err
}

func (m *fakeMachine) Schedule(action power.Action) (<-chan error, error) {
	m.done <- string(action)
	result := make(chan error, 1)
	result <- errors.New("the fake machine can't reboot") // logged, not returned
	return result, m.err
}

// withMachine makes the server's agents use machine.
func (s *server) withMachine(machine *fakeMachine) {
	s.machine.Services, s.machine.Logs, s.machine.Power = machine, machine, machine
	s.machine.Processes = fakeProcesses{machine}
}

// request sends an action to the agent and decodes the answer.
func (s *server) request(t *testing.T, agentID, action string, payload, result any) error {
	t.Helper()
	stored, err := s.fleet.AgentForHandshake(t.Context(), agentID)
	if err != nil {
		t.Fatal(err)
	}
	return s.hub.Request(t.Context(), stored.ID, action, payload, result)
}

func startWithMachine(t *testing.T, machine *fakeMachine, localPolicy policy.Policy) (*server, string) {
	t.Helper()
	s := newServer(t)
	s.withMachine(machine)
	events := s.subscribe()
	agent := s.startAgent(localPolicy)
	waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
	return s, agent.state.AgentID
}

func TestSystemRequests(t *testing.T) {
	t.Run("answers every system action", func(t *testing.T) {
		machine := newFakeMachine()
		s, agentID := startWithMachine(t, machine, policy.Policy{})
		var list protocol.ServiceListPayload
		if err := s.request(t, agentID, protocol.ActionServiceList, nil, &list); err != nil || len(list.Services) != 1 {
			t.Errorf("service.list = %+v (%v)", list, err)
		}
		var processList protocol.ProcessListPayload
		if err := s.request(t, agentID, protocol.ActionProcessList, nil, &processList); err != nil || processList.Processes[0].PID != 42 {
			t.Errorf("process.list = %+v (%v)", processList, err)
		}
		actions := []struct {
			action  string
			payload any
			did     string
		}{
			{protocol.ActionServiceStart, protocol.ServiceActionPayload{Name: "nginx.service"}, "start nginx.service"},
			{protocol.ActionServiceStop, protocol.ServiceActionPayload{Name: "nginx.service"}, "stop nginx.service"},
			{protocol.ActionServiceRestart, protocol.ServiceActionPayload{Name: "nginx.service"}, "restart nginx.service"},
			{protocol.ActionServiceEnable, protocol.ServiceActionPayload{Name: "nginx.service"}, "enable nginx.service"},
			{protocol.ActionServiceDisable, protocol.ServiceActionPayload{Name: "nginx.service"}, "disable nginx.service"},
			{protocol.ActionServiceLogs, protocol.LogQueryPayload{Service: "nginx.service"}, "logs nginx.service"},
			{protocol.ActionLogQuery, protocol.LogQueryPayload{}, "logs "},
			{protocol.ActionProcessKill, protocol.ProcessKillPayload{PID: 42, Force: true}, "kill 42 true"},
			{protocol.ActionSystemReboot, nil, "reboot"},
			{protocol.ActionSystemShutdown, nil, "shutdown"},
		}
		for _, a := range actions {
			if err := s.request(t, agentID, a.action, a.payload, nil); err != nil {
				t.Errorf("%s: %v", a.action, err)
			}
			if did := <-machine.done; did != a.did {
				t.Errorf("%s did %q, want %q", a.action, did, a.did)
			}
		}
	})

	t.Run("SEC-22: refuses to reboot or shut down when the local policy switches power off", func(t *testing.T) {
		localPolicy, err := policy.Parse("[capabilities]\npower = false\n")
		if err != nil {
			t.Fatal(err)
		}
		machine := newFakeMachine()
		s, agentID := startWithMachine(t, machine, localPolicy)
		for _, action := range []string{protocol.ActionSystemReboot, protocol.ActionSystemShutdown} {
			var agentErr *protocol.Error
			if err := s.request(t, agentID, action, nil, nil); !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied {
				t.Errorf("%s: err = %v", action, err)
			}
		}
		select {
		case did := <-machine.done:
			t.Errorf("the machine was asked to %s", did)
		default:
		}
	})

	cases := []struct {
		name, action string
		payload      any
		err          error
		code         protocol.ErrorCode
	}{
		{"an invalid service name is the server's mistake", protocol.ActionServiceStart, protocol.ServiceActionPayload{Name: "-x"}, services.ErrInvalidName, protocol.ErrorInvalidRequest},
		{"a missing service is not found", protocol.ActionServiceStop, protocol.ServiceActionPayload{Name: "x.service"}, services.ErrNotFound, protocol.ErrorNotFound},
		{"a machine without systemd is unsupported", protocol.ActionServiceList, nil, services.ErrUnsupported, protocol.ErrorUnsupportedPlatform},
		{"a bad log query is the server's mistake", protocol.ActionLogQuery, protocol.LogQueryPayload{}, logs.ErrInvalidQuery, protocol.ErrorInvalidRequest},
		{"a missing system log is unsupported", protocol.ActionLogQuery, protocol.LogQueryPayload{}, logs.ErrUnsupported, protocol.ErrorUnsupportedPlatform},
		{"a protected process is refused", protocol.ActionProcessKill, protocol.ProcessKillPayload{PID: 1}, processes.ErrProtected, protocol.ErrorInvalidRequest},
		{"a process that has gone is not found", protocol.ActionProcessKill, protocol.ProcessKillPayload{PID: 9}, processes.ErrNotFound, protocol.ErrorNotFound},
		{"a failed process list is internal", protocol.ActionProcessList, nil, errors.New("no /proc"), protocol.ErrorInternal},
		{"power on an unknown system is unsupported", protocol.ActionSystemReboot, nil, power.ErrUnsupported, protocol.ErrorUnsupportedPlatform},
		{"service.logs needs a service", protocol.ActionServiceLogs, protocol.LogQueryPayload{}, nil, protocol.ErrorInvalidRequest},
		{"a malformed service action is refused", protocol.ActionServiceRestart, "nginx", nil, protocol.ErrorInvalidRequest},
		{"a malformed log query is refused", protocol.ActionLogQuery, "today", nil, protocol.ErrorInvalidRequest},
		{"a malformed service log query is refused", protocol.ActionServiceLogs, "today", nil, protocol.ErrorInvalidRequest},
		{"a malformed kill is refused", protocol.ActionProcessKill, "42", nil, protocol.ErrorInvalidRequest},
	}
	for _, c := range cases {
		t.Run("reports errors: "+c.name, func(t *testing.T) {
			machine := newFakeMachine()
			machine.err = c.err
			s, agentID := startWithMachine(t, machine, policy.Policy{})
			var agentErr *protocol.Error
			if err := s.request(t, agentID, c.action, c.payload, nil); !errors.As(err, &agentErr) || agentErr.Code != c.code {
				t.Errorf("err = %v, want %s", err, c.code)
			}
		})
	}
}

func TestWakingAnotherMachine(t *testing.T) {
	start := func(t *testing.T, localPolicy policy.Policy, wake func(context.Context, string, string) error) (*server, string) {
		t.Helper()
		s := newServer(t)
		s.withMachine(newFakeMachine())
		s.machine.Wake = wake
		events := s.subscribe()
		agent := s.startAgent(localPolicy)
		waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
		return s, agent.state.AgentID
	}
	request := protocol.WakePayload{MAC: "aa:bb:cc:dd:ee:01", Network: "192.168.1.20/24"}

	t.Run("sends the packet it is asked to", func(t *testing.T) {
		sent := make(chan protocol.WakePayload, 1)
		s, agentID := start(t, policy.Policy{}, func(_ context.Context, mac, network string) error {
			sent <- protocol.WakePayload{MAC: mac, Network: network}
			return nil
		})
		if err := s.request(t, agentID, protocol.ActionPowerWake, request, nil); err != nil {
			t.Fatal(err)
		}
		if got := <-sent; got != request {
			t.Errorf("sent %+v", got)
		}
	})

	tests := []struct {
		name     string
		policy   string
		wakeErr  error
		wantCode protocol.ErrorCode
	}{
		{name: "SEC-22 refuses when the local policy switches power off", policy: "[capabilities]\npower = false\n", wantCode: protocol.ErrorPolicyDenied},
		{name: "refuses a network this machine isn't on", wakeErr: wol.ErrNotMyNetwork, wantCode: protocol.ErrorInvalidRequest},
		{name: "reports a packet that couldn't be sent", wakeErr: errors.New("network is unreachable"), wantCode: protocol.ErrorInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			localPolicy, err := policy.Parse(test.policy)
			if err != nil {
				t.Fatal(err)
			}
			s, agentID := start(t, localPolicy, func(context.Context, string, string) error { return test.wakeErr })
			var agentErr *protocol.Error
			if err := s.request(t, agentID, protocol.ActionPowerWake, request, nil); !errors.As(err, &agentErr) || agentErr.Code != test.wantCode {
				t.Errorf("err = %v, want %s", err, test.wantCode)
			}
		})
	}
}

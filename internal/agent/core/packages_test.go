// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/packages"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// fakePackages is a machine's package managers.
type fakePackages struct {
	supported bool
	err       error
	result    protocol.PackageApplyResult
	// applied receives each install request, and whether its context had
	// been cancelled with the connection.
	applied chan string
}

func aptUpdates() protocol.PackageUpdatesPayload {
	return protocol.PackageUpdatesPayload{Sources: []protocol.PackageSource{{
		Manager: "apt", Selectable: true, Updates: []protocol.PackageUpdate{{ID: "openssl", Name: "openssl", Available: "3.0.2", Security: true}},
	}}}
}

func (f *fakePackages) Supported() bool { return f.supported }

func (f *fakePackages) List(context.Context) (protocol.PackageUpdatesPayload, error) {
	return aptUpdates(), f.err
}

func (f *fakePackages) Apply(_ context.Context, request protocol.PackageApplyPayload) (protocol.PackageApplyResult, error) {
	if f.applied != nil {
		f.applied <- fmt.Sprintf("%s %v", request.Manager, request.IDs)
	}
	return f.result, f.err
}

func startWithPackages(t *testing.T, fake *fakePackages, policyText string) (*server, string) {
	t.Helper()
	localPolicy, err := policy.Parse(policyText)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(t)
	s.machine.Packages = fake
	events := s.subscribe()
	agent := s.startAgent(localPolicy)
	waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
	return s, agent.state.AgentID
}

func TestPackageUpdates(t *testing.T) {
	t.Run("lists the machine's updates", func(t *testing.T) {
		s, agentID := startWithPackages(t, &fakePackages{supported: true}, "")
		var got protocol.PackageUpdatesPayload
		if err := s.request(t, agentID, protocol.ActionPackageListUpdates, nil, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, aptUpdates()) {
			t.Errorf("updates = %+v", got)
		}
	})

	t.Run("installs the updates asked for", func(t *testing.T) {
		fake := &fakePackages{supported: true, applied: make(chan string, 1), result: protocol.PackageApplyResult{Output: "Done.", RestartRequired: true}}
		s, agentID := startWithPackages(t, fake, "")
		var got protocol.PackageApplyResult
		request := protocol.PackageApplyPayload{Manager: "apt", IDs: []string{"openssl"}}
		if err := s.request(t, agentID, protocol.ActionPackageApplyUpdates, request, &got); err != nil {
			t.Fatal(err)
		}
		if applied := <-fake.applied; applied != "apt [openssl]" || got != fake.result {
			t.Errorf("applied %q, answered %+v", applied, got)
		}
	})

	t.Run("switches the capability off on a machine without a package manager", func(t *testing.T) {
		s, agentID := startWithPackages(t, &fakePackages{}, "")
		stored, err := s.fleet.GetAgent(t.Context(), s.admin, agentID)
		if err != nil {
			t.Fatal(err)
		}
		for _, capability := range stored.SystemInfo.Capabilities {
			if capability.Name == policy.Packages && (capability.Enabled || capability.Reason == "") {
				t.Errorf("packages = %+v, want off with a reason", capability)
			}
		}
	})

	refusals := []struct {
		name   string
		policy string
		err    error
		action string
		want   protocol.ErrorCode
	}{
		{name: "SEC-22 refuses listing when the local policy switches packages off", policy: "[capabilities]\npackages = false\n",
			action: protocol.ActionPackageListUpdates, want: protocol.ErrorPolicyDenied},
		{name: "SEC-22 refuses installing when the local policy switches packages off", policy: "[capabilities]\npackages = false\n",
			action: protocol.ActionPackageApplyUpdates, want: protocol.ErrorPolicyDenied},
		{name: "refuses an update ID the manager wouldn't take", err: packages.ErrInvalidID,
			action: protocol.ActionPackageApplyUpdates, want: protocol.ErrorInvalidRequest},
		{name: "says when the machine has no such manager", err: packages.ErrUnknownManager,
			action: protocol.ActionPackageApplyUpdates, want: protocol.ErrorUnsupportedPlatform},
		{name: "reports a manager that failed to list", err: errors.New("apt: exit status 100"),
			action: protocol.ActionPackageListUpdates, want: protocol.ErrorInternal},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			s, agentID := startWithPackages(t, &fakePackages{supported: true, err: c.err}, c.policy)
			var agentErr *protocol.Error
			err := s.request(t, agentID, c.action, protocol.PackageApplyPayload{Manager: "apt"}, nil)
			if !errors.As(err, &agentErr) || agentErr.Code != c.want {
				t.Errorf("err = %v, want %s", err, c.want)
			}
		})
	}

	t.Run("passes on what a failed install printed", func(t *testing.T) {
		fake := &fakePackages{supported: true, err: errors.New("apt-get: exit status 100"), result: protocol.PackageApplyResult{Output: "E: Unable to lock"}}
		s, agentID := startWithPackages(t, fake, "")
		var agentErr *protocol.Error
		err := s.request(t, agentID, protocol.ActionPackageApplyUpdates, protocol.PackageApplyPayload{Manager: "apt"}, nil)
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorInternal || !strings.Contains(agentErr.Message, "E: Unable to lock") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("refuses a payload it can't read", func(t *testing.T) {
		s, agentID := startWithPackages(t, &fakePackages{supported: true}, "")
		var agentErr *protocol.Error
		err := s.request(t, agentID, protocol.ActionPackageApplyUpdates, "not an object", nil)
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorInvalidRequest {
			t.Errorf("err = %v", err)
		}
	})
}

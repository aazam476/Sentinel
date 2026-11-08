// SPDX-License-Identifier: Apache-2.0

//go:build windows

package services

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
)

// TestSCM reads the real Service Control Manager; it changes nothing.
func TestSCM(t *testing.T) {
	manager := New("windows", nil, clock.System{})

	t.Run("lists the machine's services with their state", func(t *testing.T) {
		list, err := manager.List(t.Context())
		if err != nil || list.Manager != "scm" {
			t.Fatalf("list = %s (%v)", list.Manager, err)
		}
		for _, service := range list.Services {
			if service.Name == "EventLog" {
				if service.State != protocol.ServiceRunning || service.Description == "" {
					t.Errorf("EventLog = %+v", service)
				}
				return
			}
		}
		t.Errorf("EventLog isn't among %d services", len(list.Services))
	})

	t.Run("reports a service that doesn't exist", func(t *testing.T) {
		if err := manager.Do(t.Context(), Start, "SentinelNoSuchService"); !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("refuses an invalid name", func(t *testing.T) {
		if err := manager.Do(t.Context(), Start, "-x"); !errors.Is(err, ErrInvalidName) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestSCMMapping(t *testing.T) {
	states := []struct {
		status svc.Status
		want   protocol.ServiceState
	}{
		{svc.Status{State: svc.Running}, protocol.ServiceRunning},
		{svc.Status{State: svc.Stopped}, protocol.ServiceStopped},
		{svc.Status{State: svc.Stopped, Win32ExitCode: errorNeverStarted}, protocol.ServiceStopped},
		{svc.Status{State: svc.Stopped, Win32ExitCode: 1067}, protocol.ServiceFailed},
		{svc.Status{State: svc.StartPending}, protocol.ServiceStarting},
		{svc.Status{State: svc.StopPending}, protocol.ServiceStopping},
		{svc.Status{State: svc.Paused}, protocol.ServiceUnknown},
	}
	for _, c := range states {
		if got := scmState(c.status); got != c.want {
			t.Errorf("%+v: %s, want %s", c.status, got, c.want)
		}
	}
	startups := map[uint32]protocol.ServiceStartup{
		mgr.StartAutomatic: protocol.StartupAutomatic, mgr.StartManual: protocol.StartupManual,
		mgr.StartDisabled: protocol.StartupDisabled, 0: protocol.StartupFixed,
	}
	for startType, want := range startups {
		if got := scmStartup(startType); got != want {
			t.Errorf("%d: %s, want %s", startType, got, want)
		}
	}
}

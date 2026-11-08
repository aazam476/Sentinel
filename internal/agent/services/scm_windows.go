// SPDX-License-Identifier: Apache-2.0

//go:build windows

package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/aazam476/sentinel/internal/protocol"
)

// pollInterval is how often a restart checks whether the service stopped.
const pollInterval = 250 * time.Millisecond

// errorNeverStarted is ERROR_SERVICE_NEVER_STARTED: a stopped service's
// exit code when it simply hasn't run, which isn't a failure.
const errorNeverStarted = 1077

// scm manages Windows services through the Service Control Manager's API,
// so no service name ever reaches a command line.
type scm struct {
	clock Clock
}

func newSCM(clock Clock) Manager { return scm{clock: clock} }

func (s scm) List(context.Context) (protocol.ServiceListPayload, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return protocol.ServiceListPayload{}, fmt.Errorf("connect to the service manager: %w", err)
	}
	defer manager.Disconnect() //nolint:errcheck // only releases the handle
	names, err := manager.ListServices()
	if err != nil {
		return protocol.ServiceListPayload{}, fmt.Errorf("list services: %w", err)
	}
	services := make([]protocol.Service, 0, len(names))
	for _, name := range names {
		if service, err := describe(manager, name); err == nil {
			services = append(services, service)
		} // a service removed while listing, or one we may not read, is left out
	}
	slices.SortFunc(services, func(a, b protocol.Service) int { return strings.Compare(a.Name, b.Name) })
	return protocol.ServiceListPayload{Manager: "scm", Services: services}, nil
}

func describe(manager *mgr.Mgr, name string) (protocol.Service, error) {
	service, err := manager.OpenService(name)
	if err != nil {
		return protocol.Service{}, err
	}
	defer service.Close() //nolint:errcheck // only releases the handle
	status, err := service.Query()
	if err != nil {
		return protocol.Service{}, err
	}
	config, err := service.Config()
	if err != nil {
		return protocol.Service{}, err
	}
	return protocol.Service{Name: name, Description: config.DisplayName,
		State: scmState(status), Startup: scmStartup(config.StartType)}, nil
}

func (s scm) Do(ctx context.Context, action Action, name string) error {
	if err := ValidateName("windows", name); err != nil {
		return err
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to the service manager: %w", err)
	}
	defer manager.Disconnect() //nolint:errcheck // only releases the handle
	service, err := manager.OpenService(name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer service.Close() //nolint:errcheck // only releases the handle
	return s.do(ctx, service, action)
}

func (s scm) do(ctx context.Context, service *mgr.Service, action Action) error {
	switch action {
	case Start:
		return service.Start()
	case Stop:
		_, err := service.Control(svc.Stop)
		return err
	case Restart:
		if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return err
		}
		if err := s.waitForStop(ctx, service); err != nil {
			return err
		}
		return service.Start()
	case Enable:
		return setStartType(service, mgr.StartAutomatic)
	case Disable:
		return setStartType(service, mgr.StartDisabled)
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
}

func (s scm) waitForStop(ctx context.Context, service *mgr.Service) error {
	for {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == svc.Stopped {
			return nil
		}
		timer := s.clock.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for the service to stop: %w", ctx.Err())
		case <-timer.C():
		}
	}
}

func setStartType(service *mgr.Service, startType uint32) error {
	config, err := service.Config()
	if err != nil {
		return err
	}
	config.StartType = startType
	return service.UpdateConfig(config)
}

func scmState(status svc.Status) protocol.ServiceState {
	switch status.State {
	case svc.Running:
		return protocol.ServiceRunning
	case svc.Stopped:
		if status.Win32ExitCode != 0 && status.Win32ExitCode != errorNeverStarted {
			return protocol.ServiceFailed
		}
		return protocol.ServiceStopped
	case svc.StartPending, svc.ContinuePending:
		return protocol.ServiceStarting
	case svc.StopPending, svc.PausePending:
		return protocol.ServiceStopping
	default:
		return protocol.ServiceUnknown
	}
}

func scmStartup(startType uint32) protocol.ServiceStartup {
	switch startType {
	case mgr.StartAutomatic:
		return protocol.StartupAutomatic
	case mgr.StartManual:
		return protocol.StartupManual
	case mgr.StartDisabled:
		return protocol.StartupDisabled
	default: // boot and system drivers
		return protocol.StartupFixed
	}
}

// SPDX-License-Identifier: Apache-2.0

package services_test

import (
	"errors"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/services"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/shared/clock"
	"github.com/aazam476/sentinel/internal/testutil/fakerunner"
)

const (
	listUnitFiles = "systemctl list-unit-files --type=service --no-legend --no-pager"
	listUnits     = "systemctl list-units --type=service --all --no-legend --no-pager --plain"
)

// find returns the service called name, failing the test if it is missing.
func find(t *testing.T, list []protocol.Service, name string) protocol.Service {
	t.Helper()
	for _, service := range list {
		if service.Name == name {
			return service
		}
	}
	t.Fatalf("%s isn't listed", name)
	return protocol.Service{}
}

func has(list []protocol.Service, name string) bool {
	for _, service := range list {
		if service.Name == name {
			return true
		}
	}
	return false
}

func TestSystemdList(t *testing.T) {
	cases := []struct {
		name, files, units string
		want               []protocol.Service
		absent             []string
	}{
		{
			name: "systemd 255 (Ubuntu 24.04): every state and startup mode", files: "systemd-255-list-unit-files.txt", units: "systemd-255-list-units.txt",
			want: []protocol.Service{
				{Name: "containerd.service", Description: "containerd container runtime", State: protocol.ServiceRunning, Startup: protocol.StartupAutomatic},
				{Name: "docker.service", Description: "Docker Application Container Engine", State: protocol.ServiceFailed, Startup: protocol.StartupAutomatic},
				{Name: "postgresql.service", Description: "PostgreSQL RDBMS", State: protocol.ServiceStopped, Startup: protocol.StartupAutomatic},
				{Name: "postgresql@16-main.service", Description: "PostgreSQL Cluster 16-main", State: protocol.ServiceStarting, Startup: protocol.StartupFixed},
				{Name: "redis-server.service", Description: "Advanced key-value store", State: protocol.ServiceStopping, Startup: protocol.StartupAutomatic},
				{Name: "nftables.service", Description: "nftables", State: protocol.ServiceStopped, Startup: protocol.StartupManual},
				{Name: "sudo.service", State: protocol.ServiceStopped, Startup: protocol.StartupDisabled},
				{Name: "dbus.service", Description: "D-Bus System Message Bus", State: protocol.ServiceRunning, Startup: protocol.StartupFixed},
			},
			absent: []string{"getty@.service", "redis.service", "plymouth-quit-wait.service"},
		},
		{
			name: "systemd 237 (Ubuntu 18.04): two columns and status bullets", files: "systemd-237-list-unit-files.txt", units: "systemd-237-list-units.txt",
			want: []protocol.Service{
				{Name: "nginx.service", Description: "A high performance web server and a reverse proxy server", State: protocol.ServiceFailed, Startup: protocol.StartupAutomatic},
				{Name: "ssh.service", Description: "OpenBSD Secure Shell server", State: protocol.ServiceRunning, Startup: protocol.StartupAutomatic},
				{Name: "console-getty.service", State: protocol.ServiceStopped, Startup: protocol.StartupManual},
			},
		},
		{
			name: "truncated output: the cut-off line is kept as far as it goes", files: "systemd-255-list-unit-files.txt", units: "systemd-truncated-list-units.txt",
			want: []protocol.Service{
				{Name: "containerd.service", Description: "containerd contai", State: protocol.ServiceRunning, Startup: protocol.StartupAutomatic},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := fakerunner.New(t).ExpectFile(listUnitFiles, "testdata/"+c.files).ExpectFile(listUnits, "testdata/"+c.units)
			list, err := services.NewSystemd(runner).List(t.Context())
			if err != nil || list.Manager != "systemd" {
				t.Fatalf("list = %s (%v)", list.Manager, err)
			}
			for _, want := range c.want {
				if got := find(t, list.Services, want.Name); got != want {
					t.Errorf("got %+v\nwant %+v", got, want)
				}
			}
			for _, name := range c.absent {
				if has(list.Services, name) {
					t.Errorf("%s is listed", name)
				}
			}
		})
	}
}

func TestSystemdDo(t *testing.T) {
	t.Run("runs systemctl with the name after --", func(t *testing.T) {
		for _, action := range services.Actions() {
			runner := fakerunner.New(t).Expect("systemctl "+string(action)+" --no-ask-password -- nginx.service", "", nil)
			if err := services.NewSystemd(runner).Do(t.Context(), action, "nginx.service"); err != nil {
				t.Errorf("%s: %v", action, err)
			}
		}
	})

	cases := []struct {
		name, service string
		action        services.Action
		answer        error
		want          error
	}{
		{"refuses a name that would read as an option", "--now.service", services.Start, nil, services.ErrInvalidName},
		{"refuses a unit that isn't a service", "reboot.target", services.Start, nil, services.ErrInvalidName},
		{"refuses a name with a shell metacharacter", "a;b.service", services.Start, nil, services.ErrInvalidName},
		{"refuses services that reboot the machine (SEC-22)", "systemd-reboot.service", services.Start, nil, services.ErrPowerService},
		{"reports a service that doesn't exist", "nope.service", services.Restart, errors.New("exit status 5: failed to restart nope.service: Unit nope.service not found"), services.ErrNotFound},
		{"reports a machine without systemd", "nginx.service", services.Stop, command.ErrNotFound, services.ErrUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := fakerunner.New(t)
			if c.answer != nil {
				runner.Expect("systemctl "+string(c.action)+" --no-ask-password -- "+c.service, "", c.answer)
			}
			if err := services.NewSystemd(runner).Do(t.Context(), c.action, c.service); !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}

	t.Run("refuses an unknown action", func(t *testing.T) {
		if err := services.NewSystemd(fakerunner.New(t)).Do(t.Context(), "mask", "nginx.service"); err == nil {
			t.Error("Do returned nil")
		}
	})

	t.Run("reports a machine without systemd when listing", func(t *testing.T) {
		runner := fakerunner.New(t).Expect(listUnitFiles, "", command.ErrNotFound)
		if _, err := services.NewSystemd(runner).List(t.Context()); !errors.Is(err, services.ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
		runner = fakerunner.New(t).ExpectFile(listUnitFiles, "testdata/systemd-255-list-unit-files.txt").Expect(listUnits, "", errors.New("Failed to connect to bus"))
		if _, err := services.NewSystemd(runner).List(t.Context()); err == nil {
			t.Error("List returned nil")
		}
	})
}

func TestLaunchdList(t *testing.T) {
	cases := []struct {
		name, list, disabled string
		want                 []protocol.Service
		absent               []string
	}{
		{
			name: "macOS 14: running, stopped, crashed and disabled jobs", list: "macos-14-launchctl-list.txt", disabled: "macos-14-print-disabled.txt",
			want: []protocol.Service{
				{Name: "com.openssh.sshd", State: protocol.ServiceRunning, Startup: protocol.StartupAutomatic},
				{Name: "com.example.crashy", State: protocol.ServiceFailed, Startup: protocol.StartupAutomatic},
				{Name: "com.example.badconfig", State: protocol.ServiceFailed, Startup: protocol.StartupAutomatic},
				{Name: "com.apple.mDNSResponder", State: protocol.ServiceRunning, Startup: protocol.StartupAutomatic},
				{Name: "com.apple.ftpd", State: protocol.ServiceStopped, Startup: protocol.StartupDisabled},
			},
			absent: []string{"PID", "com.apple.xpc.launchd.oneshot.0x10000001.Some"},
		},
		{
			name: "macOS 10.14: disabled written as true", list: "macos-14-launchctl-list.txt", disabled: "macos-10.14-print-disabled.txt",
			want: []protocol.Service{
				{Name: "com.apple.ftpd", State: protocol.ServiceStopped, Startup: protocol.StartupDisabled},
				{Name: "com.openssh.sshd", State: protocol.ServiceRunning, Startup: protocol.StartupAutomatic},
			},
		},
		{
			name: "truncated output: a cut-off label is still listed", list: "macos-truncated-launchctl-list.txt", disabled: "macos-14-print-disabled.txt",
			want: []protocol.Service{{Name: "com.opens", State: protocol.ServiceRunning, Startup: protocol.StartupAutomatic}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := fakerunner.New(t).ExpectFile("launchctl list", "testdata/"+c.list).ExpectFile("launchctl print-disabled system", "testdata/"+c.disabled)
			list, err := services.NewLaunchd(runner).List(t.Context())
			if err != nil || list.Manager != "launchd" {
				t.Fatalf("list = %s (%v)", list.Manager, err)
			}
			for _, want := range c.want {
				if got := find(t, list.Services, want.Name); got != want {
					t.Errorf("got %+v\nwant %+v", got, want)
				}
			}
			for _, name := range c.absent {
				if has(list.Services, name) {
					t.Errorf("%s is listed", name)
				}
			}
		})
	}

	t.Run("reports launchctl failing", func(t *testing.T) {
		runner := fakerunner.New(t).Expect("launchctl list", "", command.ErrNotFound)
		if _, err := services.NewLaunchd(runner).List(t.Context()); !errors.Is(err, services.ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
		runner = fakerunner.New(t).Expect("launchctl list", "", nil).Expect("launchctl print-disabled system", "", errors.New("exit status 1"))
		if _, err := services.NewLaunchd(runner).List(t.Context()); err == nil {
			t.Error("List returned nil")
		}
	})
}

func TestLaunchdDo(t *testing.T) {
	commands := map[services.Action]string{
		services.Start:   "launchctl kickstart system/com.openssh.sshd",
		services.Stop:    "launchctl kill SIGTERM system/com.openssh.sshd",
		services.Restart: "launchctl kickstart -k system/com.openssh.sshd",
		services.Enable:  "launchctl enable system/com.openssh.sshd",
		services.Disable: "launchctl disable system/com.openssh.sshd",
	}
	for action, commandLine := range commands {
		t.Run(string(action)+" runs "+commandLine, func(t *testing.T) {
			runner := fakerunner.New(t).Expect(commandLine, "", nil)
			if err := services.NewLaunchd(runner).Do(t.Context(), action, "com.openssh.sshd"); err != nil {
				t.Error(err)
			}
		})
	}

	cases := []struct {
		name, label string
		action      services.Action
		answer      error
		want        error
	}{
		{"refuses a label that would read as an option", "-k", services.Start, nil, services.ErrInvalidName},
		{"refuses a label with a slash", "../user/501/x", services.Start, nil, services.ErrInvalidName},
		{"reports a job that doesn't exist", "com.example.nope", services.Start, errors.New("exit status 113: Could not find service \"com.example.nope\" in domain for system"), services.ErrNotFound},
		{"passes other failures on", "com.example.x", services.Enable, errors.New("exit status 1: Operation not permitted"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := fakerunner.New(t)
			if c.answer != nil {
				runner.Expect("launchctl "+string(c.action)+" system/"+c.label, "", c.answer)
				if c.action == services.Start {
					runner.Expect("launchctl kickstart system/"+c.label, "", c.answer)
				}
			}
			err := services.NewLaunchd(runner).Do(t.Context(), c.action, c.label)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}

	t.Run("refuses an unknown action", func(t *testing.T) {
		if err := services.NewLaunchd(fakerunner.New(t)).Do(t.Context(), "bootout", "com.example.x"); err == nil {
			t.Error("Do returned nil")
		}
	})
}

func TestNew(t *testing.T) {
	t.Run("picks the service manager for each system", func(t *testing.T) {
		runner := fakerunner.New(t)
		if _, ok := services.New("linux", runner, clock.System{}).(services.Systemd); !ok {
			t.Error("linux isn't systemd")
		}
		if _, ok := services.New("darwin", runner, clock.System{}).(services.Launchd); !ok {
			t.Error("darwin isn't launchd")
		}
	})

	t.Run("reports systems it doesn't know as unsupported", func(t *testing.T) {
		manager := services.New("plan9", fakerunner.New(t), clock.System{})
		if _, err := manager.List(t.Context()); !errors.Is(err, services.ErrUnsupported) {
			t.Errorf("List: %v", err)
		}
		if err := manager.Do(t.Context(), services.Start, "x"); !errors.Is(err, services.ErrUnsupported) {
			t.Errorf("Do: %v", err)
		}
		if err := services.ValidateName("plan9", "x"); !errors.Is(err, services.ErrUnsupported) {
			t.Errorf("ValidateName: %v", err)
		}
	})
}

func TestValidateName(t *testing.T) {
	cases := []struct {
		goos, name string
		valid      bool
	}{
		{"linux", "getty@tty1.service", true},
		{"linux", `systemd-fsck@dev-disk-by\x2duuid-1234.service`, true},
		{"linux", "nginx", false},
		{"linux", "nginx.service\n", false},
		{"linux", "nginx service.service", false},
		{"darwin", "com.openssh.sshd", true},
		{"darwin", `com.example."quoted"`, false},
		{"windows", "MSSQL$SQLEXPRESS", true},
		{"windows", "Windows Update Medic", true},
		{"windows", "x' or '1'='1", false},
		{"windows", " leading-space", false},
		{"windows", string(make([]byte, 257)), false},
	}
	for _, c := range cases {
		t.Run(c.goos+" "+c.name, func(t *testing.T) {
			if err := services.ValidateName(c.goos, c.name); (err == nil) != c.valid {
				t.Errorf("err = %v, want valid = %v", err, c.valid)
			}
		})
	}
}

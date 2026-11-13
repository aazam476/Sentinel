// SPDX-License-Identifier: Apache-2.0

package packages_test

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/packages"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/testutil/fakerunner"
)

const (
	wingetPath   = `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.22_x64__8wekyb3d8bbwe\winget.exe`
	powershell   = "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "
	rebootScript = "(New-Object -ComObject Microsoft.Update.SystemInfo).RebootRequired"
	brew         = "/usr/bin/sudo -u ada -H -- /opt/homebrew/bin/brew "
	aptGet       = "env DEBIAN_FRONTEND=noninteractive apt-get -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold "
)

// host is a machine with these programs and files.
func host(goos string, runner *fakerunner.Runner, programs ...string) packages.Host {
	has := func(name string) bool {
		for _, program := range programs {
			if program == name {
				return true
			}
		}
		return false
	}
	return packages.Host{
		GOOS: goos, Runner: runner,
		Find: func(name string) string {
			if name == `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_*_x64__8wekyb3d8bbwe\winget.exe` && has("winget") {
				return wingetPath
			}
			if has(name) {
				return "/usr/bin/" + name
			}
			return ""
		},
		Exists: has,
		Owner:  func(string) (string, error) { return "ada", nil },
	}
}

func golden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestList(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		programs []string
		answers  map[string]string
		want     []protocol.PackageSource
		restart  bool
	}{
		{
			name: "on Debian, refreshes apt and marks security updates", goos: "linux",
			programs: []string{"apt-get", "dnf", "/var/run/reboot-required"},
			answers:  map[string]string{"apt-get update -qq": "", "apt list --upgradable": "apt.txt"},
			restart:  true,
			want: []protocol.PackageSource{{Manager: "apt", Selectable: true, Updates: []protocol.PackageUpdate{
				{ID: "openssl", Name: "openssl", Current: "3.0.2-0ubuntu1.14", Available: "3.0.2-0ubuntu1.15", Security: true},
				{ID: "tzdata", Name: "tzdata", Current: "2023c-0ubuntu0.22.04.2", Available: "2024a-0ubuntu0.22.04.1"},
				{ID: "libstdc++6", Name: "libstdc++6", Current: "12.1.0-2ubuntu1~22.04", Available: "12.3.0-1ubuntu1~22.04"},
			}}},
		},
		{
			name: "on Fedora, lists dnf's upgrades without the architecture", goos: "linux", programs: []string{"dnf"},
			answers: map[string]string{"dnf -q --refresh list --upgrades": "dnf.txt"},
			want: []protocol.PackageSource{{Manager: "dnf", Selectable: true, Updates: []protocol.PackageUpdate{
				{ID: "kernel", Name: "kernel", Available: "6.8.9-300.fc40"},
				{ID: "python3-libs", Name: "python3-libs", Available: "3.12.3-2.fc40"},
			}}},
		},
		{
			name: "on openSUSE, reads zypper's table", goos: "linux", programs: []string{"zypper"},
			answers: map[string]string{"zypper --non-interactive --quiet refresh": "", "zypper --non-interactive --quiet list-updates": "zypper.txt"},
			want: []protocol.PackageSource{{Manager: "zypper", Selectable: true, Updates: []protocol.PackageUpdate{
				{ID: "curl", Name: "curl", Current: "8.6.0-1.1", Available: "8.7.1-1.1"},
				{ID: "libcurl4", Name: "libcurl4", Current: "8.6.0-1.1", Available: "8.7.1-1.1"},
			}}},
		},
		{
			name: "on Arch, lists with checkupdates and only updates everything", goos: "linux", programs: []string{"pacman"},
			answers: map[string]string{"checkupdates": "pacman.txt"},
			want: []protocol.PackageSource{{Manager: "pacman", Updates: []protocol.PackageUpdate{
				{ID: "linux", Name: "linux", Current: "6.8.9.arch1-1", Available: "6.9.1.arch1-1"},
				{ID: "openssl", Name: "openssl", Current: "3.3.0-1", Available: "3.3.1-1"},
			}}},
		},
		{
			name: "on Alpine, splits apk's names from their versions", goos: "linux", programs: []string{"apk"},
			answers: map[string]string{"apk update --quiet": "", "apk list --upgradable": "apk.txt"},
			want: []protocol.PackageSource{{Manager: "apk", Selectable: true, Updates: []protocol.PackageUpdate{
				{ID: "busybox", Name: "busybox", Current: "1.36.1-r28", Available: "1.36.1-r29"},
				{ID: "libcrypto3", Name: "libcrypto3", Current: "3.3.0-r2", Available: "3.3.1-r0"},
			}}},
		},
		{
			name: "on macOS, lists softwareupdate and Homebrew as its owner, leaving out pinned formulae", goos: "darwin",
			programs: []string{"/opt/homebrew/bin/brew"},
			answers: map[string]string{
				"/usr/sbin/softwareupdate --list": "softwareupdate.txt",
				brew + "update --quiet":           "", brew + "outdated --json=v2": "homebrew.json",
			},
			want: []protocol.PackageSource{
				{Manager: "softwareupdate", Selectable: true, Updates: []protocol.PackageUpdate{
					{ID: "Command Line Tools for Xcode-15.3", Name: "Command Line Tools for Xcode", Available: "15.3"},
					{ID: "macOS Sonoma 14.5-23F79", Name: "macOS Sonoma 14.5", Available: "14.5"},
				}},
				{Manager: "homebrew", Selectable: true, Updates: []protocol.PackageUpdate{
					{ID: "git", Name: "git", Current: "2.44.0", Available: "2.45.1"},
					{ID: "firefox", Name: "firefox", Current: "125.0.3", Available: "126.0"},
				}},
			},
		},
		{
			name: "on Windows, lists Windows Update and winget, past winget's spinner", goos: "windows", programs: []string{"winget"},
			answers: map[string]string{
				powershell + rebootScript: "False\r\n",
				wingetPath + " upgrade --accept-source-agreements --disable-interactivity": "winget.txt",
			},
			want: []protocol.PackageSource{
				{Manager: "windows-update", Selectable: true, Updates: []protocol.PackageUpdate{
					{ID: "0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d", Name: "2024-05 Cumulative Update for Windows 11 Version 23H2 for x64-based Systems (KB5037771)", Security: true},
					{ID: "9f8e7d6c-5b4a-3928-1706-f5e4d3c2b1a0", Name: "Windows Malicious Software Removal Tool x64 - v5.124 (KB890830)"},
				}},
				{Manager: "winget", Selectable: true, Updates: []protocol.PackageUpdate{
					{ID: "Microsoft.Edge", Name: "Microsoft Edge", Current: "124.0.2478.80", Available: "125.0.2535.51"},
					{ID: "Microsoft.VisualStudioCode", Name: "Visual Studio Code éèà", Current: "1.88.1", Available: "1.89.1"},
				}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := fakerunner.New(t)
			for line, file := range c.answers {
				if strings.Contains(file, ".") {
					runner.ExpectFile(line, "testdata/"+file)
				} else {
					runner.Expect(line, file, nil)
				}
			}
			if c.goos == "windows" {
				runner.Expect(powershell+searchScript(t), golden(t, "windows-update.json"), nil)
			}
			got, err := packages.New(host(c.goos, runner, c.programs...)).List(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Sources, c.want) || got.RestartRequired != c.restart {
				t.Errorf("List() =\n%+v\nwant\n%+v (restart %v)", got, c.want, c.restart)
			}
		})
	}
}

// searchScript is the Windows Update search script, read from the code's
// own testdata copy so the test doesn't repeat it.
func searchScript(t *testing.T) string {
	t.Helper()
	return packages.SearchScript
}

func TestListFailures(t *testing.T) {
	t.Run("says when the machine has no package manager it knows", func(t *testing.T) {
		service := packages.New(host("linux", fakerunner.New(t)))
		if _, err := service.List(t.Context()); !errors.Is(err, packages.ErrUnsupported) || service.Supported() {
			t.Errorf("List() error = %v, Supported() = %v", err, service.Supported())
		}
		if _, err := packages.New(host("plan9", fakerunner.New(t))).List(t.Context()); !errors.Is(err, packages.ErrUnsupported) {
			t.Errorf("List() on plan9 = %v", err)
		}
	})

	t.Run("shows a manager's error and still lists the others", func(t *testing.T) {
		runner := fakerunner.New(t).
			Expect("/usr/sbin/softwareupdate --list", "", errors.New("softwareupdate: exit status 1: can't reach Apple")).
			Expect(brew+"update --quiet", "", errors.New("offline")).
			Expect(brew+"outdated --json=v2", `{"formulae":[],"casks":[]}`, nil)
		got, err := packages.New(host("darwin", runner, "/opt/homebrew/bin/brew")).List(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got.Sources[0].Error == "" || len(got.Sources[0].Updates) != 0 || got.Sources[1].Error != "" {
			t.Errorf("List() = %+v", got)
		}
	})

	t.Run("treats checkupdates' exit code 2 as nothing to update", func(t *testing.T) {
		runner := fakerunner.New(t).Expect("checkupdates", "", errors.New("checkupdates: exit status 2"))
		got, err := packages.New(host("linux", runner, "pacman")).List(t.Context())
		if err != nil || got.Sources[0].Error != "" || len(got.Sources[0].Updates) != 0 {
			t.Errorf("List() = %+v, %v", got, err)
		}
	})

	t.Run("leaves out Homebrew when root owns it, since brew won't run as root", func(t *testing.T) {
		machine := host("darwin", fakerunner.New(t).Expect("/usr/sbin/softwareupdate --list", "No new software available.\n", nil), "/usr/local/bin/brew")
		machine.Owner = func(string) (string, error) { return "root", nil }
		got, err := packages.New(machine).List(t.Context())
		if err != nil || len(got.Sources) != 1 {
			t.Errorf("List() = %+v, %v", got, err)
		}
	})
}

func TestApply(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		programs []string
		request  protocol.PackageApplyPayload
		command  string
	}{
		{name: "upgrades every apt package", goos: "linux", programs: []string{"apt-get"},
			request: protocol.PackageApplyPayload{Manager: "apt"}, command: aptGet + "dist-upgrade"},
		{name: "upgrades chosen apt packages only", goos: "linux", programs: []string{"apt-get"},
			request: protocol.PackageApplyPayload{Manager: "apt", IDs: []string{"openssl", "libstdc++6"}},
			command: aptGet + "install --only-upgrade -- openssl libstdc++6"},
		{name: "upgrades chosen dnf packages", goos: "linux", programs: []string{"dnf"},
			request: protocol.PackageApplyPayload{Manager: "dnf", IDs: []string{"kernel"}}, command: "dnf -y upgrade -- kernel"},
		{name: "upgrades every zypper package", goos: "linux", programs: []string{"zypper"},
			request: protocol.PackageApplyPayload{Manager: "zypper"}, command: "zypper --non-interactive update"},
		{name: "upgrades Arch all at once", goos: "linux", programs: []string{"pacman"},
			request: protocol.PackageApplyPayload{Manager: "pacman"}, command: "pacman -Syu --noconfirm"},
		{name: "upgrades chosen apk packages", goos: "linux", programs: []string{"apk"},
			request: protocol.PackageApplyPayload{Manager: "apk", IDs: []string{"busybox"}}, command: "apk add --upgrade --no-interactive -- busybox"},
		{name: "upgrades every apk package", goos: "linux", programs: []string{"apk"},
			request: protocol.PackageApplyPayload{Manager: "apk"}, command: "apk upgrade --no-interactive"},
		{name: "installs a macOS update by its label", goos: "darwin",
			request: protocol.PackageApplyPayload{Manager: "softwareupdate", IDs: []string{"macOS Sonoma 14.5-23F79"}},
			command: "/usr/sbin/softwareupdate --install macOS Sonoma 14.5-23F79"},
		{name: "installs every macOS update", goos: "darwin",
			request: protocol.PackageApplyPayload{Manager: "softwareupdate"}, command: "/usr/sbin/softwareupdate --install --all"},
		{name: "upgrades Homebrew as its owner", goos: "darwin", programs: []string{"/opt/homebrew/bin/brew"},
			request: protocol.PackageApplyPayload{Manager: "homebrew", IDs: []string{"git"}}, command: brew + "upgrade -- git"},
		{name: "upgrades every winget package", goos: "windows", programs: []string{"winget"},
			request: protocol.PackageApplyPayload{Manager: "winget"},
			command: wingetPath + " upgrade --silent --accept-package-agreements --accept-source-agreements --disable-interactivity --all"},
		{name: "upgrades a chosen winget package", goos: "windows", programs: []string{"winget"},
			request: protocol.PackageApplyPayload{Manager: "winget", IDs: []string{"Microsoft.Edge"}},
			command: wingetPath + " upgrade --silent --accept-package-agreements --accept-source-agreements --disable-interactivity --exact --id Microsoft.Edge"},
		{name: "installs chosen Windows updates by ID", goos: "windows",
			request: protocol.PackageApplyPayload{Manager: "windows-update", IDs: []string{"0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d"}},
			command: powershell + "$wanted = @('0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d')\n" + packages.InstallScript},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := fakerunner.New(t).Expect(c.command, "Done.\n", nil)
			if c.goos == "windows" {
				runner.Expect(powershell+rebootScript, "True\r\n", nil)
			}
			result, err := packages.New(host(c.goos, runner, c.programs...)).Apply(t.Context(), c.request)
			if err != nil || result.Output != "Done.\n" || result.RestartRequired != (c.goos == "windows") {
				t.Errorf("Apply() = %+v, %v", result, err)
			}
		})
	}
}

func TestApplyRefusals(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		programs []string
		request  protocol.PackageApplyPayload
		want     error
	}{
		{name: "refuses a name that reads as an option", goos: "linux", programs: []string{"apt-get"},
			request: protocol.PackageApplyPayload{Manager: "apt", IDs: []string{"--allow-unauthenticated"}}, want: packages.ErrInvalidID},
		{name: "refuses a name with a space", goos: "linux", programs: []string{"dnf"},
			request: protocol.PackageApplyPayload{Manager: "dnf", IDs: []string{"kernel extra"}}, want: packages.ErrInvalidID},
		{name: "refuses a Windows update ID that isn't a GUID, since it goes into a script", goos: "windows",
			request: protocol.PackageApplyPayload{Manager: "windows-update", IDs: []string{"'); Remove-Item C:\\ -Recurse; ('"}}, want: packages.ErrInvalidID},
		{name: "refuses a very long name", goos: "linux", programs: []string{"apk"},
			request: protocol.PackageApplyPayload{Manager: "apk", IDs: []string{strings.Repeat("a", 257)}}, want: packages.ErrInvalidID},
		{name: "refuses choosing packages on Arch", goos: "linux", programs: []string{"pacman"},
			request: protocol.PackageApplyPayload{Manager: "pacman", IDs: []string{"linux"}}, want: packages.ErrNotSelectable},
		{name: "refuses a manager the machine doesn't have", goos: "linux", programs: []string{"apt-get"},
			request: protocol.PackageApplyPayload{Manager: "dnf"}, want: packages.ErrUnknownManager},
		{name: "refuses on a machine without a manager", goos: "linux",
			request: protocol.PackageApplyPayload{Manager: "apt"}, want: packages.ErrUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := packages.New(host(c.goos, fakerunner.New(t), c.programs...)).Apply(t.Context(), c.request)
			if !errors.Is(err, c.want) {
				t.Errorf("Apply() = %v, want %v", err, c.want)
			}
		})
	}

	t.Run("returns the end of the output when the manager fails", func(t *testing.T) {
		long := strings.Repeat("x", 70<<10) + "E: Unable to lock the administration directory\n"
		runner := fakerunner.New(t).Expect(aptGet+"dist-upgrade", long, errors.New("apt-get: exit status 100"))
		result, err := packages.New(host("linux", runner, "apt-get")).Apply(t.Context(), protocol.PackageApplyPayload{Manager: "apt"})
		if err == nil || len(result.Output) != 64<<10 || !strings.HasSuffix(result.Output, "administration directory\n") {
			t.Errorf("Apply() = %d bytes, %v", len(result.Output), err)
		}
	})

	t.Run("stops at the first winget package that fails", func(t *testing.T) {
		base := wingetPath + " upgrade --silent --accept-package-agreements --accept-source-agreements --disable-interactivity --exact --id "
		runner := fakerunner.New(t).Expect(base+"A.One", "one\n", nil).Expect(base+"B.Two", "two\n", errors.New("exit status 1")).
			Expect(powershell+rebootScript, "False", nil)
		result, err := packages.New(host("windows", runner, "winget")).Apply(t.Context(), protocol.PackageApplyPayload{Manager: "winget", IDs: []string{"A.One", "B.Two", "C.Three"}})
		if err == nil || result.Output != "one\ntwo\n" {
			t.Errorf("Apply() = %+v, %v", result, err)
		}
	})
}

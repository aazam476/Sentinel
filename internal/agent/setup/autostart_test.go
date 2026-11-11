// SPDX-License-Identifier: Apache-2.0

package setup_test

import (
	"encoding/xml"
	"slices"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/setup"
)

// fakeAutostart records the tray's sign-in entry being added and removed.
type fakeAutostart struct {
	calls []string
	fail  bool
}

func (a *fakeAutostart) do(call string) error {
	a.calls = append(a.calls, call)
	if a.fail {
		return errBroken
	}
	return nil
}

func (a *fakeAutostart) Install() error   { return a.do("install") }
func (a *fakeAutostart) Uninstall() error { return a.do("uninstall") }

func TestTheTrayStartsAtSignInOnceInstalled(t *testing.T) {
	tests := []struct {
		name       string
		fail       bool
		wantOutput string
	}{
		{name: "adds the sign-in entry after the service", wantOutput: "for everyone who signs in"},
		{name: "reports a sign-in entry it can't add, and still installs", fail: true, wantOutput: "won't start at sign-in (broken)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			autostart := &fakeAutostart{fail: test.fail}
			f.options.Autostart = autostart
			if err := setup.Install(t.Context(), f.options, server()); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(autostart.calls, []string{"install"}) || !strings.Contains(f.output.String(), test.wantOutput) {
				t.Errorf("calls %v, output %q", autostart.calls, f.output.String())
			}
		})
	}

	t.Run("removes it on uninstall", func(t *testing.T) {
		for _, fail := range []bool{false, true} {
			f := newFixture(t)
			autostart := &fakeAutostart{fail: fail}
			f.options.Autostart = autostart
			if err := setup.Uninstall(f.options); err != nil {
				t.Fatal(err)
			}
			want := "no longer starts at sign-in"
			if fail {
				want = "sign-in entry wasn't removed"
			}
			if !slices.Equal(autostart.calls, []string{"uninstall"}) || !strings.Contains(f.output.String(), want) {
				t.Errorf("calls %v, output %q", autostart.calls, f.output.String())
			}
		}
	})

	t.Run("a dry run only says so", func(t *testing.T) {
		f := newFixture(t)
		autostart := &fakeAutostart{}
		f.options.Autostart, f.options.DryRun = autostart, true
		if err := setup.Install(t.Context(), f.options, server()); err != nil {
			t.Fatal(err)
		}
		if len(autostart.calls) != 0 || !strings.Contains(f.output.String(), "Would start the Sentinel tray") {
			t.Errorf("calls %v, output %q", autostart.calls, f.output.String())
		}
	})
}

func TestTheSignInEntriesStartTheTray(t *testing.T) {
	t.Run("the macOS launch agent is valid XML naming the program", func(t *testing.T) {
		plist := setup.LaunchAgentPlist("/Library/Sentinel/bin/sentinel-agent & co")
		decoder := xml.NewDecoder(strings.NewReader(plist))
		decoder.Strict = false // the DOCTYPE isn't fetched
		for {
			if _, err := decoder.Token(); err != nil {
				if err.Error() != "EOF" {
					t.Fatalf("plist: %v", err)
				}
				break
			}
		}
		for _, want := range []string{"<string>/Library/Sentinel/bin/sentinel-agent &amp; co</string>", "<string>tray</string>", "<string>Aqua</string>"} {
			if !strings.Contains(plist, want) {
				t.Errorf("plist lacks %s", want)
			}
		}
	})
	t.Run("the Windows Run entry quotes the program", func(t *testing.T) {
		if got := setup.RunCommand(`C:\Program Files\Sentinel\sentinel-agent.exe`); got != `"C:\Program Files\Sentinel\sentinel-agent.exe" tray` {
			t.Errorf("command = %s", got)
		}
	})
}

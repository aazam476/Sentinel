// SPDX-License-Identifier: Apache-2.0

package setup_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/core"
	"github.com/aazam476/sentinel/internal/agent/identity"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/setup"
)

var errBroken = errors.New("broken")

// fakeService records what was done to it.
type fakeService struct {
	calls []string
	fail  string
}

func (s *fakeService) do(call string) error {
	s.calls = append(s.calls, call)
	if call == s.fail {
		return errBroken
	}
	return nil
}

func (s *fakeService) Install() error   { return s.do("install") }
func (s *fakeService) Uninstall() error { return s.do("uninstall") }
func (s *fakeService) Start() error     { return s.do("start") }
func (s *fakeService) Stop() error      { return s.do("stop") }

type fixture struct {
	options setup.Options
	service *fakeService
	output  *strings.Builder
	// enrolled records what Enroll was asked.
	enrolled []core.EnrollOptions
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{service: &fakeService{}, output: &strings.Builder{}}
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	f.options = setup.Options{
		Paths:   identity.Paths{StateDir: filepath.Join(root, "state"), ConfigDir: filepath.Join(root, "etc")},
		Service: f.service,
		Enroll: func(_ context.Context, options core.EnrollOptions) (identity.State, error) {
			f.enrolled = append(f.enrolled, options)
			return identity.State{ServerURL: options.ServerURL, AgentID: "01JBX3K5Q7Z8M4N6P8R0T2V4W6",
				ServerPublicKey: public, TLSFingerprint: options.TLSFingerprint}, nil
		},
		Hostname: func() (string, error) { return "nas-01", nil },
		Version:  "v0.1.0",
		Out:      f.output,
	}
	return f
}

func server() setup.Server {
	return setup.Server{URL: "https://sentinel.example.com", Token: "token", Fingerprint: "SHA256:server", TLSFingerprint: "SHA256:tls"}
}

func TestInstall(t *testing.T) {
	t.Run("enrolls, saves the identity and a starter policy, and starts the service", func(t *testing.T) {
		f := newFixture(t)
		if err := setup.Install(t.Context(), f.options, server()); err != nil {
			t.Fatal(err)
		}
		if len(f.enrolled) != 1 || f.enrolled[0].Hostname != "nas-01" || f.enrolled[0].Token != "token" || len(f.enrolled[0].Key) == 0 {
			t.Errorf("enrolled with %+v", f.enrolled)
		}
		store, err := identity.Open(f.options.Paths.StateDir)
		if err != nil {
			t.Fatal(err)
		}
		state, err := store.Load()
		if err != nil || state.TLSFingerprint != "SHA256:tls" {
			t.Errorf("saved %+v (%v)", state, err)
		}
		loaded, err := policy.Load(f.options.Paths.PolicyFile())
		if err != nil || !loaded.Allows(policy.Terminal) {
			t.Errorf("the starter policy didn't load with everything allowed: %v", err)
		}
		if !slices.Equal(f.service.calls, []string{"install", "start"}) {
			t.Errorf("service calls = %v", f.service.calls)
		}
	})

	t.Run("keeps an admin's existing policy file", func(t *testing.T) {
		f := newFixture(t)
		if err := os.MkdirAll(f.options.Paths.ConfigDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.options.Paths.PolicyFile(), []byte("[capabilities]\nterminal = false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := setup.Install(t.Context(), f.options, server()); err != nil {
			t.Fatal(err)
		}
		loaded, err := policy.Load(f.options.Paths.PolicyFile())
		if err != nil || loaded.Allows(policy.Terminal) {
			t.Errorf("the admin's policy was replaced (%v)", err)
		}
	})

	t.Run("a dry run only describes the steps and doesn't use the token", func(t *testing.T) {
		f := newFixture(t)
		f.options.DryRun = true
		if err := setup.Install(t.Context(), f.options, server()); err != nil {
			t.Fatal(err)
		}
		if len(f.enrolled) != 0 || len(f.service.calls) != 0 {
			t.Errorf("a dry run enrolled %d times and made service calls %v", len(f.enrolled), f.service.calls)
		}
		if _, err := os.Stat(f.options.Paths.StateDir); !errors.Is(err, os.ErrNotExist) {
			t.Error("a dry run created the state directory")
		}
		if !strings.Contains(f.output.String(), "Would enroll with https://sentinel.example.com") {
			t.Errorf("output = %q", f.output.String())
		}
	})

	t.Run("refuses to enroll a machine twice", func(t *testing.T) {
		f := newFixture(t)
		if err := setup.Enroll(t.Context(), f.options, server()); err != nil {
			t.Fatal(err)
		}
		if err := setup.Install(t.Context(), f.options, server()); !errors.Is(err, setup.ErrAlreadyEnrolled) {
			t.Errorf("err = %v, want ErrAlreadyEnrolled", err)
		}
	})

	failures := []struct {
		name    string
		breakIt func(f *fixture)
	}{
		{"enrollment", func(f *fixture) {
			f.options.Enroll = func(context.Context, core.EnrollOptions) (identity.State, error) {
				return identity.State{}, core.ErrWrongServer
			}
		}},
		{"reading the hostname", func(f *fixture) { f.options.Hostname = func() (string, error) { return "", errBroken } }},
		{"installing the service", func(f *fixture) { f.service.fail = "install" }},
		{"starting the service", func(f *fixture) { f.service.fail = "start" }},
		{"saving an invalid identity", func(f *fixture) {
			f.options.Enroll = func(context.Context, core.EnrollOptions) (identity.State, error) { return identity.State{}, nil }
		}},
		{"opening the state directory", func(f *fixture) {
			blocker := filepath.Join(filepath.Dir(f.options.Paths.StateDir), "file")
			_ = os.WriteFile(blocker, nil, 0o600)
			f.options.Paths.StateDir = filepath.Join(blocker, "state")
		}},
		{"writing the policy file", func(f *fixture) {
			_ = os.WriteFile(f.options.Paths.ConfigDir, nil, 0o600) // a file where the directory should be
		}},
	}
	for _, failure := range failures {
		t.Run("reports a failure "+failure.name, func(t *testing.T) {
			f := newFixture(t)
			failure.breakIt(f)
			if err := setup.Install(t.Context(), f.options, server()); err == nil {
				t.Error("Install returned nil")
			}
		})
	}
}

func TestUninstall(t *testing.T) {
	t.Run("stops and removes the service and deletes state and configuration", func(t *testing.T) {
		f := newFixture(t)
		if err := setup.Install(t.Context(), f.options, server()); err != nil {
			t.Fatal(err)
		}
		f.service.calls = nil
		if err := setup.Uninstall(t.Context(), f.options); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(f.service.calls, []string{"stop", "uninstall"}) {
			t.Errorf("service calls = %v", f.service.calls)
		}
		for _, dir := range []string{f.options.Paths.StateDir, f.options.Paths.ConfigDir} {
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s still exists", dir)
			}
		}
	})

	t.Run("carries on when the service was never installed", func(t *testing.T) {
		f := newFixture(t)
		f.service.fail = "uninstall"
		if err := setup.Uninstall(t.Context(), f.options); err != nil {
			t.Errorf("err = %v", err)
		}
		if !strings.Contains(f.output.String(), "may not have been installed") {
			t.Errorf("output = %q", f.output.String())
		}
	})

	t.Run("a dry run changes nothing", func(t *testing.T) {
		f := newFixture(t)
		if err := setup.Install(t.Context(), f.options, server()); err != nil {
			t.Fatal(err)
		}
		f.service.calls = nil
		f.options.DryRun = true
		if err := setup.Uninstall(t.Context(), f.options); err != nil {
			t.Fatal(err)
		}
		if len(f.service.calls) != 0 {
			t.Errorf("service calls = %v", f.service.calls)
		}
		if _, err := os.Stat(f.options.Paths.StateDir); err != nil {
			t.Error("a dry run deleted the state")
		}
	})
}

// TestStarterPolicyExamples checks every example in the starter policy is
// a setting the agent accepts, once its # is removed.
func TestStarterPolicyExamples(t *testing.T) {
	var lines []string
	for line := range strings.Lines(setup.StarterPolicy(runtime.GOOS)) {
		example := strings.TrimPrefix(line, "# ")
		if strings.HasPrefix(example, "[") || strings.Contains(example, " = ") {
			lines = append(lines, example)
		} else {
			lines = append(lines, line)
		}
	}
	if _, err := policy.Parse(strings.Join(lines, "")); err != nil {
		t.Errorf("the starter policy's examples don't parse: %v", err)
	}
}

func TestStarterPolicyFitsItsSystem(t *testing.T) {
	if linux := setup.StarterPolicy("linux"); !strings.Contains(linux, `"/srv"`) || !strings.Contains(linux, "xauthority") {
		t.Error("the Linux starter policy lacks its examples")
	}
	if windows := setup.StarterPolicy("windows"); !strings.Contains(windows, `'D:\Shares'`) || strings.Contains(windows, "xauthority") {
		t.Error("the Windows starter policy has the wrong examples")
	}
	if darwin := setup.StarterPolicy("darwin"); strings.Contains(darwin, "xauthority") {
		t.Error("the macOS starter policy offers X11 settings")
	}
}

func TestUninstallTellsTheServerFirst(t *testing.T) {
	tests := []struct {
		name       string
		report     error
		wantOutput string
	}{
		{name: "says the server was told", wantOutput: "Told the server this machine was removed"},
		{name: "says how to finish by hand when the server can't be reached", report: errBroken, wantOutput: "revoke it on its page"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			if err := setup.Install(t.Context(), f.options, server()); err != nil {
				t.Fatal(err)
			}
			stateWasThere := false
			f.options.ReportRemoved = func(context.Context) error {
				// The key is still there to sign the report with.
				_, err := os.Stat(f.options.Paths.StateDir)
				stateWasThere = err == nil
				return test.report
			}
			if err := setup.Uninstall(t.Context(), f.options); err != nil {
				t.Fatal(err)
			}
			if !stateWasThere || !strings.Contains(f.output.String(), test.wantOutput) {
				t.Errorf("state there when reporting: %v; output = %q", stateWasThere, f.output.String())
			}
		})
	}
	t.Run("a dry run only says it would", func(t *testing.T) {
		f := newFixture(t)
		f.options.DryRun = true
		f.options.ReportRemoved = func(context.Context) error {
			t.Error("a dry run told the server")
			return nil
		}
		if err := setup.Uninstall(t.Context(), f.options); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(f.output.String(), "Would tell the server") {
			t.Errorf("output = %q", f.output.String())
		}
	})
}

// SPDX-License-Identifier: Apache-2.0

package stacks_test

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/stacks"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/testutil/fakerunner"
)

const ls = "docker compose ls --all --format json"

// lsOutput is the captured `docker compose ls` with the stacks folder of
// the test in place of /opt/sentinel/stacks.
func lsOutput(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/compose-5.6-ls.json")
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "/opt/sentinel/stacks", filepath.ToSlash(dir))
}

// withStack creates a stack folder with a compose file and .env.
func withStack(t *testing.T, dir, name, compose, env string) {
	t.Helper()
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "compose.yaml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	if env != "" {
		if err := os.WriteFile(filepath.Join(folder, ".env"), []byte(env), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// blogProject is the compose flags for the test stack "blog" in dir.
func blogProject(dir string) string {
	folder := filepath.Join(dir, "blog")
	return "--project-name blog --project-directory " + folder + " --file " + filepath.Join(folder, "compose.yaml")
}

func TestList(t *testing.T) {
	t.Run("lists Sentinel's stacks and every other compose project", func(t *testing.T) {
		dir := t.TempDir()
		withStack(t, dir, "blog", "services: {}\n", "")
		withStack(t, dir, "drafts", "services: {}\n", "") // never started
		if err := os.MkdirAll(filepath.Join(dir, "empty-folder"), 0o700); err != nil {
			t.Fatal(err)
		}
		runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil)
		list, err := stacks.New(runner, dir, "linux").List(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]protocol.Stack{}
		for _, stack := range list.Stacks {
			got[stack.Name] = stack
		}
		if names := slices.Sorted(maps.Keys(got)); !slices.Equal(names, []string{"blog", "drafts", "media", "orphan"}) {
			t.Fatalf("stacks = %v", names)
		}
		if blog := got["blog"]; !blog.Managed || !blog.Editable || blog.Containers["running"] != 2 {
			t.Errorf("blog = %+v", blog)
		}
		if drafts := got["drafts"]; !drafts.Managed || len(drafts.Containers) != 0 {
			t.Errorf("drafts = %+v", drafts)
		}
		media := got["media"]
		if media.Managed || media.Editable || media.Containers["exited"] != 1 || media.Containers["running"] != 2 || len(media.ConfigFiles) != 2 {
			t.Errorf("media = %+v", media)
		}
		if orphan := got["orphan"]; len(orphan.ConfigFiles) != 0 || orphan.Editable {
			t.Errorf("orphan = %+v", orphan)
		}
		if list.Dir != dir {
			t.Errorf("dir = %s", list.Dir)
		}
	})

	t.Run("reports a machine without docker compose", func(t *testing.T) {
		runner := fakerunner.New(t).Expect(ls, "", command.ErrNotFound)
		if _, err := stacks.New(runner, t.TempDir(), "linux").List(t.Context()); !errors.Is(err, stacks.ErrUnavailable) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("knows where each system keeps stacks", func(t *testing.T) {
		if stacks.DefaultDir("linux") != "/opt/sentinel/stacks" || !strings.Contains(stacks.DefaultDir("windows"), "ProgramData") ||
			!strings.Contains(stacks.DefaultDir("darwin"), "Application Support") {
			t.Error("unexpected default folders")
		}
	})
}

func TestGet(t *testing.T) {
	for _, fixture := range []string{"compose-5.6-ps.ndjson", "compose-2.20-ps.json"} {
		t.Run("returns a stack's files and containers ("+fixture+")", func(t *testing.T) {
			dir := t.TempDir()
			withStack(t, dir, "blog", "services:\n  web: {}\n", "QUEUE=jobs\n")
			ps, err := os.ReadFile("testdata/" + fixture)
			if err != nil {
				t.Fatal(err)
			}
			runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil).
				Expect("docker compose --project-name blog ps --all --format json", string(ps), nil)
			files, err := stacks.New(runner, dir, "linux").Get(t.Context(), "blog")
			if err != nil {
				t.Fatal(err)
			}
			if files.Compose != "services:\n  web: {}\n" || files.Env != "QUEUE=jobs\n" || !files.Editable || !files.Managed {
				t.Errorf("files = %+v", files)
			}
			if len(files.Services) != 2 || files.Services[0].Service != "web" || files.Services[0].Ports != "127.0.0.1:8081->80/tcp" {
				t.Errorf("services = %+v", files.Services)
			}
		})
	}

	t.Run("refuses a name that isn't a stack, or isn't a name", func(t *testing.T) {
		dir := t.TempDir()
		runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil)
		manager := stacks.New(runner, dir, "linux")
		if _, err := manager.Get(t.Context(), "nope"); !errors.Is(err, stacks.ErrNotFound) {
			t.Errorf("nope: %v", err)
		}
		for _, name := range []string{"../etc", "Blog", "-p", "", strings.Repeat("a", 64)} {
			if _, err := manager.Get(t.Context(), name); !errors.Is(err, stacks.ErrInvalid) {
				t.Errorf("%q: %v", name, err)
			}
		}
	})
}

func TestSave(t *testing.T) {
	validate := func(dir, name string, withEnv bool) string {
		folder := filepath.Join(dir, name)
		line := "docker compose --project-name " + name + " --project-directory " + folder + " --file " + filepath.Join(folder, "compose.yaml.new")
		if withEnv {
			line += " --env-file " + filepath.Join(folder, ".env.new")
		}
		return line + " config --quiet"
	}

	t.Run("creates a stack from a pasted compose file, readable by root only", func(t *testing.T) {
		dir := t.TempDir()
		runner := fakerunner.New(t).Expect(ls, "[]", nil).Expect(validate(dir, "shop", true), "", nil)
		request := protocol.StackSavePayload{Name: "shop", Compose: "services:\n  db: {image: postgres}\n", Env: "PASSWORD=hunter2\n"}
		if err := stacks.New(runner, dir, "linux").Save(t.Context(), request); err != nil {
			t.Fatal(err)
		}
		for file, want := range map[string]string{"compose.yaml": request.Compose, ".env": request.Env} {
			path := filepath.Join(dir, "shop", file)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != want {
				t.Errorf("%s = %q (%v)", file, data, err)
			}
			if info, err := os.Stat(path); runtime.GOOS != "windows" && (err != nil || info.Mode().Perm() != 0o600) {
				t.Errorf("%s mode = %v (%v)", file, info.Mode(), err)
			}
		}
		if entries, _ := os.ReadDir(filepath.Join(dir, "shop")); len(entries) != 2 {
			t.Errorf("left behind: %v", entries)
		}
	})

	t.Run("checks without saving, and refuses what compose rejects", func(t *testing.T) {
		dir := t.TempDir()
		withStack(t, dir, "blog", "old\n", "OLD=1\n")
		invalid := errors.New("exit status 15: services.web.image must be a string")
		runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil).
			Expect(validate(dir, "blog", false), "", nil)
		manager := stacks.New(runner, dir, "linux")
		if err := manager.Save(t.Context(), protocol.StackSavePayload{Name: "blog", Compose: "new\n", ValidateOnly: true}); err != nil {
			t.Fatal(err)
		}
		runner.Expect(validate(dir, "blog", false), "", invalid)
		if err := manager.Save(t.Context(), protocol.StackSavePayload{Name: "blog", Compose: "bad\n"}); !errors.Is(err, stacks.ErrInvalid) || !strings.Contains(err.Error(), "must be a string") {
			t.Errorf("err = %v", err)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "blog", "compose.yaml"))
		env, _ := os.ReadFile(filepath.Join(dir, "blog", ".env"))
		if string(data) != "old\n" || string(env) != "OLD=1\n" {
			t.Errorf("files changed: %q %q", data, env)
		}
	})

	t.Run("removes the .env when the new one is empty", func(t *testing.T) {
		dir := t.TempDir()
		withStack(t, dir, "blog", "old\n", "OLD=1\n")
		runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil).Expect(validate(dir, "blog", false), "", nil)
		if err := stacks.New(runner, dir, "linux").Save(t.Context(), protocol.StackSavePayload{Name: "blog", Compose: "new\n"}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "blog", ".env")); !os.IsNotExist(err) {
			t.Errorf(".env still there: %v", err)
		}
	})

	t.Run("edits another project's compose file where it is", func(t *testing.T) {
		dir, elsewhere := t.TempDir(), t.TempDir()
		external := filepath.Join(elsewhere, "docker-compose.yml")
		if err := os.WriteFile(external, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		listing := `[{"Name":"media","Status":"running(1)","ConfigFiles":"` + filepath.ToSlash(external) + `"}]`
		check := "docker compose --project-name media --project-directory " + elsewhere + " --file " + filepath.Join(elsewhere, "compose.yaml.new") + " config --quiet"
		runner := fakerunner.New(t).Expect(ls, listing, nil).Expect(check, "", nil)
		if err := stacks.New(runner, dir, "linux").Save(t.Context(), protocol.StackSavePayload{Name: "media", Compose: "new\n"}); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(external); string(data) != "new\n" {
			t.Errorf("compose file = %q", data)
		}
	})

	t.Run("refuses a project whose file the agent can't read, and oversized files", func(t *testing.T) {
		dir := t.TempDir()
		runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil)
		manager := stacks.New(runner, dir, "linux")
		if err := manager.Save(t.Context(), protocol.StackSavePayload{Name: "media", Compose: "x"}); !errors.Is(err, stacks.ErrReadOnly) {
			t.Errorf("media: %v", err)
		}
		if err := manager.Save(t.Context(), protocol.StackSavePayload{Name: "big", Compose: strings.Repeat("x", 257<<10)}); !errors.Is(err, stacks.ErrInvalid) {
			t.Errorf("big: %v", err)
		}
		if err := manager.Save(t.Context(), protocol.StackSavePayload{Name: "../x", Compose: "x"}); !errors.Is(err, stacks.ErrInvalid) {
			t.Errorf("../x: %v", err)
		}
	})
}

func TestUpDownPull(t *testing.T) {
	t.Run("runs compose with the stack's own files", func(t *testing.T) {
		dir := t.TempDir()
		withStack(t, dir, "blog", "services: {}\n", "")
		runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil).
			Expect("docker compose "+blogProject(dir)+" up --detach --remove-orphans", "", nil).
			Expect("docker compose "+blogProject(dir)+" down", "", nil).
			Expect("docker compose "+blogProject(dir)+" pull", "", nil)
		manager := stacks.New(runner, dir, "linux")
		for name, run := range map[string]func() error{
			"up":   func() error { return manager.Up(t.Context(), "blog") },
			"down": func() error { return manager.Down(t.Context(), "blog") },
			"pull": func() error { return manager.Pull(t.Context(), "blog") },
		} {
			if err := run(); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
		calls := strings.Join(runner.Calls(), "\n")
		if strings.Count(calls, " up --detach") != 2 { // up, and pull's redeploy
			t.Errorf("calls:\n%s", calls)
		}
	})

	t.Run("controls other projects with the files compose reported", func(t *testing.T) {
		dir := t.TempDir()
		runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil).
			Expect("docker compose --project-name media --file /srv/media/docker-compose.yml --file /srv/media/docker-compose.override.yml up --detach --remove-orphans", "", nil).
			Expect("docker compose --project-name orphan down", "", nil)
		manager := stacks.New(runner, dir, "linux")
		if err := manager.Up(t.Context(), "media"); err != nil {
			t.Error(err)
		}
		if err := manager.Down(t.Context(), "orphan"); err != nil {
			t.Error(err)
		}
		if err := manager.Up(t.Context(), "orphan"); !errors.Is(err, stacks.ErrReadOnly) {
			t.Errorf("up orphan: %v", err)
		}
	})

	t.Run("passes on compose's error", func(t *testing.T) {
		dir := t.TempDir()
		withStack(t, dir, "blog", "services: {}\n", "")
		runner := fakerunner.New(t).Expect(ls, lsOutput(t, dir), nil).
			Expect("docker compose "+blogProject(dir)+" pull", "", errors.New("exit status 18: pull access denied"))
		if err := stacks.New(runner, dir, "linux").Pull(t.Context(), "blog"); err == nil || !strings.Contains(err.Error(), "pull access denied") {
			t.Errorf("err = %v", err)
		}
	})
}

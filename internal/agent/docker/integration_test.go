// SPDX-License-Identifier: Apache-2.0

//go:build integration

package docker_test

import (
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/docker"
	"github.com/aazam476/sentinel/internal/protocol"
)

// TestRealDocker runs against the machine's Docker, when it has one: CI's
// Linux runners do. It starts a container and drives it through the agent.
func TestRealDocker(t *testing.T) {
	engine := docker.New("", runtime.GOOS)
	if _, err := engine.Describe(t.Context()); errors.Is(err, docker.ErrUnavailable) {
		t.Skip("no Docker on this machine")
	}
	name := "sentinel-integration-test"
	_ = exec.CommandContext(t.Context(), "docker", "rm", "-f", name).Run() // left over from a failed run
	run := exec.CommandContext(t.Context(), "docker", "run", "-d", "--name", name, "alpine:3.20",
		"sh", "-c", "echo started; sleep 300")
	if output, err := run.CombinedOutput(); err != nil {
		t.Skipf("can't start a test container (no image or registry access?): %s", output)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() }) //nolint:noctx // the test's context has ended

	list, err := engine.Containers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := slices.ContainsFunc(list.Containers, func(c protocol.Container) bool {
		return c.Name == name && c.State == "running"
	})
	if !found {
		t.Fatalf("%s isn't listed as running", name)
	}
	logs, err := engine.Logs(t.Context(), protocol.ContainerLogsPayload{ID: name})
	if err != nil || len(logs.Lines) == 0 || logs.Lines[0].Text != "started" {
		t.Errorf("logs = %+v (%v)", logs, err)
	}
	for _, action := range []docker.ContainerAction{docker.Restart, docker.Stop, docker.Start} {
		if err := engine.DoContainer(t.Context(), action, protocol.ContainerPayload{ID: name}); err != nil {
			t.Errorf("%s: %v", action, err)
		}
	}
	if err := engine.DoContainer(t.Context(), docker.Remove, protocol.ContainerPayload{ID: name}); !errors.Is(err, docker.ErrConflict) {
		t.Errorf("removing a running container: %v", err)
	}
	if err := engine.DoContainer(t.Context(), docker.Remove, protocol.ContainerPayload{ID: name, Force: true}); err != nil {
		t.Errorf("forced remove: %v", err)
	}
}

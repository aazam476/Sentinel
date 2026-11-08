// SPDX-License-Identifier: Apache-2.0

//go:build integration

package stacks_test

import (
	"os/exec"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/agent/stacks"
	"github.com/aazam476/sentinel/internal/protocol"
)

// TestRealCompose creates, starts and removes a stack with the machine's
// docker compose, when it has a working Docker (as CI's Linux runners do).
func TestRealCompose(t *testing.T) {
	if exec.CommandContext(t.Context(), "docker", "info").Run() != nil {
		t.Skip("no Docker on this machine")
	}
	if exec.CommandContext(t.Context(), "docker", "image", "inspect", "alpine:3.20").Run() != nil &&
		exec.CommandContext(t.Context(), "docker", "pull", "alpine:3.20").Run() != nil {
		t.Skip("can't get alpine:3.20 (no registry access?)")
	}
	manager := stacks.New(command.Exec{}, t.TempDir(), "linux")
	name := "sentinel-integration"
	t.Cleanup(func() { _ = manager.Down(t.Context(), name) })

	compose := "services:\n  app:\n    image: alpine:3.20\n    command: [\"sh\", \"-c\", \"echo $$GREETING; sleep 300\"]\n    environment:\n      GREETING: ${GREETING}\n"
	if err := manager.Save(t.Context(), protocol.StackSavePayload{Name: name, Compose: "services: [", ValidateOnly: true}); err == nil {
		t.Error("an invalid compose file passed validation")
	}
	if err := manager.Save(t.Context(), protocol.StackSavePayload{Name: name, Compose: compose, Env: "GREETING=hello\n"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Up(t.Context(), name); err != nil {
		t.Fatal(err)
	}
	files, err := manager.Get(t.Context(), name)
	if err != nil || len(files.Services) != 1 || files.Services[0].State != "running" || files.Env != "GREETING=hello\n" {
		t.Errorf("files = %+v (%v)", files, err)
	}
	if err := manager.Down(t.Context(), name); err != nil {
		t.Error(err)
	}
}

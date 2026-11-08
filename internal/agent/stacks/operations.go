// SPDX-License-Identifier: Apache-2.0

package stacks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aazam476/sentinel/internal/protocol"
)

// Get returns a stack's files and containers. Another project's compose
// file is shown if the agent can read it; its .env isn't.
func (m *Manager) Get(ctx context.Context, name string) (protocol.StackFilesPayload, error) {
	stack, err := m.find(ctx, name)
	if err != nil {
		return protocol.StackFilesPayload{}, err
	}
	files := protocol.StackFilesPayload{Name: name, Managed: stack.Managed, Editable: stack.Editable,
		Services: []protocol.StackService{}}
	if stack.Editable {
		if files.Compose, err = readFile(stack.ConfigFiles[0], maxComposeSize); err != nil {
			return protocol.StackFilesPayload{}, err
		}
	}
	if stack.Managed {
		if files.Env, err = readFile(filepath.Join(m.dir, name, envFile), maxEnvSize); err != nil {
			return protocol.StackFilesPayload{}, err
		}
	}
	output, err := m.compose(ctx, "--project-name", name, "ps", "--all", "--format", "json")
	if err != nil {
		return protocol.StackFilesPayload{}, fmt.Errorf("list the containers of stack %s: %w", name, err)
	}
	if files.Services, err = ParseServices(output); err != nil {
		return protocol.StackFilesPayload{}, err
	}
	return files, nil
}

// Save checks a stack's files with `docker compose config` and, unless
// only validating, writes them. A new name creates the stack in Sentinel's
// folder; another project's compose file is written back where it is.
func (m *Manager) Save(ctx context.Context, request protocol.StackSavePayload) error {
	if err := checkName(request.Name); err != nil {
		return err
	}
	m.saving.Lock()
	defer m.saving.Unlock()
	if len(request.Compose) > maxComposeSize || len(request.Env) > maxEnvSize {
		return fmt.Errorf("%w: the compose file is limited to %d KB and .env to %d KB", ErrInvalid, maxComposeSize>>10, maxEnvSize>>10)
	}
	stack, err := m.find(ctx, request.Name)
	if err != nil {
		stack = protocol.Stack{Name: request.Name, Managed: true, Editable: true} // a new stack
	}
	if !stack.Editable {
		return fmt.Errorf("%s: %w", request.Name, ErrReadOnly)
	}
	folder := filepath.Join(m.dir, request.Name)
	if !stack.Managed {
		folder = filepath.Dir(stack.ConfigFiles[0])
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return fmt.Errorf("create the stack's folder: %w", err)
	}
	staged, err := m.stage(folder, request)
	defer staged.remove()
	if err != nil {
		return err
	}
	if err := m.validate(ctx, request.Name, folder, staged); err != nil || request.ValidateOnly {
		return err
	}
	return staged.commit(folder, stack)
}

// validate runs `docker compose config` on the staged files.
func (m *Manager) validate(ctx context.Context, name, folder string, staged stagedFiles) error {
	args := []string{"--project-name", name, "--project-directory", folder, "--file", staged.compose}
	if staged.env != "" {
		args = append(args, "--env-file", staged.env)
	}
	if _, err := m.compose(ctx, append(args, "config", "--quiet")...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

// Up starts or updates a stack's containers.
func (m *Manager) Up(ctx context.Context, name string) error {
	return m.run(ctx, name, "up", "--detach", "--remove-orphans")
}

// Down stops and removes a stack's containers and networks (not volumes).
func (m *Manager) Down(ctx context.Context, name string) error {
	return m.run(ctx, name, "down")
}

// Pull pulls newer images for a stack and redeploys it with them.
func (m *Manager) Pull(ctx context.Context, name string) error {
	if err := m.run(ctx, name, "pull"); err != nil {
		return err
	}
	return m.Up(ctx, name)
}

func (m *Manager) run(ctx context.Context, name string, verb ...string) error {
	stack, err := m.find(ctx, name)
	if err != nil {
		return err
	}
	if !stack.Managed && len(stack.ConfigFiles) == 0 && verb[0] != "down" {
		return fmt.Errorf("%w: compose doesn't know where %s's files are", ErrReadOnly, name)
	}
	if _, err := m.compose(ctx, append(m.projectArgs(stack), verb...)...); err != nil {
		return fmt.Errorf("%s stack %s: %w", verb[0], name, err)
	}
	return nil
}

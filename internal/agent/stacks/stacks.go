// SPDX-License-Identifier: Apache-2.0

package stacks

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/aazam476/sentinel/internal/agent/command"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Limits on the files a stack may have.
const (
	maxComposeSize = 256 << 10
	maxEnvSize     = 64 << 10
)

// File names inside a stack's folder.
const (
	composeFile = "compose.yaml"
	envFile     = ".env"
)

// Errors from Manager. They are never reassigned.
var (
	// ErrUnavailable: the docker command or its compose plugin is missing.
	ErrUnavailable = errors.New("docker compose not found")
	// ErrNotFound: no stack has that name.
	ErrNotFound = errors.New("there is no stack with that name")
	// ErrInvalid: the name or files can't be used.
	ErrInvalid = errors.New("invalid stack")
	// ErrReadOnly: the stack's compose file can't be read, so it can't be
	// edited from Sentinel.
	ErrReadOnly = errors.New("this stack's compose file isn't readable by the agent")
)

// stackName is what compose accepts as a project name. It is also a folder
// name, so it can't climb out of the stacks folder.
var stackName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// Manager runs compose stacks: Sentinel's own, one folder each in Dir with
// compose.yaml and .env, and any others `docker compose ls` finds.
type Manager struct {
	runner command.Runner
	dir    string
	// saving lets one save run at a time, as they stage files under fixed
	// names.
	saving sync.Mutex
}

// New returns a Manager keeping stacks in dir, or in the system's default
// folder when dir is empty.
func New(runner command.Runner, dir, goos string) *Manager {
	if dir == "" {
		dir = DefaultDir(goos)
	}
	return &Manager{runner: runner, dir: dir}
}

// DefaultDir is where stacks live unless the policy file says otherwise.
func DefaultDir(goos string) string {
	switch goos {
	case "windows":
		return `C:\ProgramData\Sentinel\stacks`
	case "darwin":
		return "/Library/Application Support/Sentinel/stacks"
	default:
		return "/opt/sentinel/stacks"
	}
}

func checkName(name string) error {
	if !stackName.MatchString(name) {
		return fmt.Errorf("%w: %q isn't a valid stack name (lowercase letters, digits, - and _)", ErrInvalid, name)
	}
	return nil
}

// compose runs `docker compose` with args.
func (m *Manager) compose(ctx context.Context, args ...string) ([]byte, error) {
	output, err := m.runner.Run(ctx, "docker", append([]string{"compose"}, args...)...)
	if errors.Is(err, command.ErrNotFound) {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return output, err
}

// List finds Sentinel's stacks and every other compose project.
func (m *Manager) List(ctx context.Context) (protocol.StackListPayload, error) {
	output, err := m.compose(ctx, "ls", "--all", "--format", "json")
	if err != nil {
		return protocol.StackListPayload{}, fmt.Errorf("list stacks: %w", err)
	}
	projects, err := ParseProjects(output)
	if err != nil {
		return protocol.StackListPayload{}, err
	}
	stacks := map[string]protocol.Stack{}
	for _, name := range m.managedNames() {
		stacks[name] = protocol.Stack{Name: name, Managed: true, Editable: true, Containers: map[string]int{},
			ConfigFiles: []string{filepath.Join(m.dir, name, composeFile)}}
	}
	for _, project := range projects {
		stack, managed := stacks[project.Name]
		if !managed {
			stack = protocol.Stack{Name: project.Name, ConfigFiles: project.ConfigFiles,
				Editable: len(project.ConfigFiles) > 0 && readable(project.ConfigFiles[0])}
		}
		stack.Containers = project.Containers
		stacks[project.Name] = stack
	}
	list := protocol.StackListPayload{Dir: m.dir, Stacks: make([]protocol.Stack, 0, len(stacks))}
	for _, stack := range stacks {
		list.Stacks = append(list.Stacks, stack)
	}
	slices.SortFunc(list.Stacks, func(a, b protocol.Stack) int { return strings.Compare(a.Name, b.Name) })
	return list, nil
}

// managedNames lists the folders in Dir that hold a compose file.
func (m *Manager) managedNames() []string {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil // no folder yet: no stacks
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && checkName(entry.Name()) == nil && regular(filepath.Join(m.dir, entry.Name(), composeFile)) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func regular(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func readable(path string) bool {
	file, err := os.Open(path) //nolint:gosec // G304: a compose file compose itself reported
	if err != nil {
		return false
	}
	_ = file.Close() // only opened to check
	return true
}

// find returns the stack called name, or ErrNotFound.
func (m *Manager) find(ctx context.Context, name string) (protocol.Stack, error) {
	if err := checkName(name); err != nil {
		return protocol.Stack{}, err
	}
	list, err := m.List(ctx)
	if err != nil {
		return protocol.Stack{}, err
	}
	index := slices.IndexFunc(list.Stacks, func(stack protocol.Stack) bool { return stack.Name == name })
	if index < 0 {
		return protocol.Stack{}, fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	return list.Stacks[index], nil
}

// projectArgs are the compose flags naming a stack and its files.
func (m *Manager) projectArgs(stack protocol.Stack) []string {
	args := []string{"--project-name", stack.Name}
	if stack.Managed {
		folder := filepath.Join(m.dir, stack.Name)
		return append(args, "--project-directory", folder, "--file", filepath.Join(folder, composeFile))
	}
	for _, file := range stack.ConfigFiles {
		args = append(args, "--file", file)
	}
	return args
}

// readFile reads a stack file, or "" if it doesn't exist.
func readFile(path string, limit int64) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return "", fmt.Errorf("%w: %s isn't a regular file under %d KB", ErrInvalid, path, limit>>10)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: inside the stacks folder or reported by compose
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

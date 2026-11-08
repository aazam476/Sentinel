// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"

	"github.com/aazam476/sentinel/internal/protocol"
)

// pingTimeout limits how long finding a working socket may take.
const pingTimeout = 3 * time.Second

// Errors from Engine. They are never reassigned.
var (
	// ErrUnavailable: no Docker or Podman socket answers.
	ErrUnavailable = errors.New("docker not detected")
	// ErrNotFound: no container, image, volume or network has that name.
	ErrNotFound = errors.New("not found")
	// ErrInvalid: the name or reference can't be valid.
	ErrInvalid = errors.New("invalid name")
	// ErrConflict: Docker refused because of the thing's state, such as
	// removing a running container or a volume in use.
	ErrConflict = errors.New("refused by docker")
)

// Engine talks to the machine's container engine. It connects on first
// use and again after the engine restarts.
type Engine struct {
	candidates []string

	mutex  sync.Mutex
	client *client.Client
	engine protocol.DockerEngine
}

// New returns an Engine for the socket in the policy, or for the usual
// sockets of the operating system (runtime.GOOS) if it names none.
func New(socket, goos string) *Engine {
	if socket != "" {
		return &Engine{candidates: []string{socket}}
	}
	return &Engine{candidates: defaultSockets(goos)}
}

// defaultSockets are where Docker and Podman listen when run as a system
// service; Docker first, as the more common.
func defaultSockets(goos string) []string {
	switch goos {
	case "windows":
		return []string{"npipe:////./pipe/docker_engine", "npipe:////./pipe/podman-machine-default"}
	case "darwin":
		return []string{"unix:///var/run/docker.sock"}
	default:
		return []string{"unix:///var/run/docker.sock", "unix:///run/podman/podman.sock"}
	}
}

// connect returns a client for a socket that answers, keeping it for
// later calls.
func (e *Engine) connect(ctx context.Context) (*client.Client, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.client != nil {
		return e.client, nil
	}
	for _, socket := range e.candidates {
		if path, isUnix := strings.CutPrefix(socket, "unix://"); isUnix {
			if _, err := os.Stat(path); err != nil {
				continue // nothing listens there; skip the slow dial
			}
		}
		connected, engine, err := dial(ctx, socket)
		if err == nil {
			e.client, e.engine = connected, engine
			return connected, nil
		}
	}
	return nil, fmt.Errorf("%w (looked for %s)", ErrUnavailable, strings.Join(e.candidates, ", "))
}

func dial(ctx context.Context, socket string) (*client.Client, protocol.DockerEngine, error) {
	connected, err := client.New(client.WithHost(socket))
	if err != nil {
		return nil, protocol.DockerEngine{}, fmt.Errorf("docker client for %s: %w", socket, err)
	}
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	version, err := connected.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		_ = connected.Close() // it never worked
		return nil, protocol.DockerEngine{}, fmt.Errorf("docker at %s: %w", socket, err)
	}
	name := "Docker"
	if strings.Contains(strings.ToLower(version.Platform.Name), "podman") {
		name = "Podman"
	}
	return connected, protocol.DockerEngine{Name: name, Version: version.Version, Socket: socket}, nil
}

// call runs fn with a connected client and turns Docker's errors into this
// package's. A connection error forgets the client, so the next call looks
// for the engine again (it may have restarted, or moved socket).
func (e *Engine) call(ctx context.Context, fn func(*client.Client) error) error {
	connected, err := e.connect(ctx)
	if err != nil {
		return err
	}
	err = fn(connected)
	switch {
	case err == nil:
		return nil
	case cerrdefs.IsNotFound(err):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case cerrdefs.IsConflict(err), cerrdefs.IsFailedPrecondition(err):
		return fmt.Errorf("%w: %w", ErrConflict, err)
	case cerrdefs.IsInvalidArgument(err):
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	case client.IsErrConnectionFailed(err):
		e.forget(connected)
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	default:
		return err
	}
}

func (e *Engine) forget(failed *client.Client) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.client == failed {
		_ = e.client.Close() // already broken
		e.client = nil
	}
}

// Describe reports which engine the agent uses.
func (e *Engine) Describe(ctx context.Context) (protocol.DockerEngine, error) {
	if _, err := e.connect(ctx); err != nil {
		return protocol.DockerEngine{}, err
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.engine, nil
}

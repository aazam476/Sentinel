// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aazam476/sentinel/internal/agent/docker"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/stacks"
	"github.com/aazam476/sentinel/internal/agent/terminal"
	"github.com/aazam476/sentinel/internal/protocol"
)

// dockerHandlers answers the Docker actions (§9.4). Every one checks the
// local policy first: listing too, since even names of containers can say
// more than the machine's owner wants shared (SEC-22).
func (c *connection) dockerHandlers() map[string]handler {
	containerAction := func(action docker.ContainerAction) handler {
		return dockerCall(c, func(ctx context.Context, request protocol.ContainerPayload) (any, error) {
			return nil, c.agent.options.Docker.DoContainer(ctx, action, request)
		})
	}
	engine := func() Docker { return c.agent.options.Docker }
	return map[string]handler{
		protocol.ActionDockerContainerList:    dockerList(c, func(ctx context.Context) (any, error) { return engine().Containers(ctx) }),
		protocol.ActionDockerContainerStart:   containerAction(docker.Start),
		protocol.ActionDockerContainerStop:    containerAction(docker.Stop),
		protocol.ActionDockerContainerRestart: containerAction(docker.Restart),
		protocol.ActionDockerContainerRemove:  containerAction(docker.Remove),
		protocol.ActionDockerContainerInspect: dockerCall(c, func(ctx context.Context, r protocol.ContainerPayload) (any, error) {
			return engine().Inspect(ctx, r.ID)
		}),
		protocol.ActionDockerContainerLogs: dockerCall(c, c.containerLogs),
		protocol.ActionDockerContainerExec: dockerCall(c, func(_ context.Context, r protocol.ContainerExecPayload) (any, error) {
			return c.execInContainer(r)
		}),
		protocol.ActionDockerImageList: dockerList(c, func(ctx context.Context) (any, error) { return engine().Images(ctx) }),
		protocol.ActionDockerImagePull: dockerCall(c, func(ctx context.Context, r protocol.ImageRefPayload) (any, error) {
			return engine().Pull(ctx, r)
		}),
		protocol.ActionDockerImageRemove: dockerCall(c, func(ctx context.Context, r protocol.ImageRefPayload) (any, error) {
			return nil, engine().RemoveImage(ctx, r)
		}),
		protocol.ActionDockerImageCheck: dockerCall(c, func(ctx context.Context, r protocol.ImageRefPayload) (any, error) {
			return engine().CheckImage(ctx, r)
		}),
		protocol.ActionDockerVolumeList: dockerList(c, func(ctx context.Context) (any, error) { return engine().Volumes(ctx) }),
		protocol.ActionDockerVolumeRemove: dockerCall(c, func(ctx context.Context, r protocol.NamePayload) (any, error) {
			return nil, engine().RemoveVolume(ctx, r.Name)
		}),
		protocol.ActionDockerNetworkList: dockerList(c, func(ctx context.Context) (any, error) { return engine().Networks(ctx) }),
		protocol.ActionDockerNetworkRemove: dockerCall(c, func(ctx context.Context, r protocol.NamePayload) (any, error) {
			return nil, engine().RemoveNetwork(ctx, r.Name)
		}),
	}
}

// stackHandlers answers the compose stack actions, under the same policy
// switch as the rest of Docker.
func (c *connection) stackHandlers() map[string]handler {
	manager := func() Stacks { return c.agent.options.Stacks }
	stackAction := func(run func(Stacks, context.Context, string) error) handler {
		return dockerCall(c, func(ctx context.Context, r protocol.StackPayload) (any, error) {
			return nil, run(manager(), ctx, r.Name)
		})
	}
	return map[string]handler{
		protocol.ActionDockerStackList: dockerList(c, func(ctx context.Context) (any, error) { return manager().List(ctx) }),
		protocol.ActionDockerStackGet: dockerCall(c, func(ctx context.Context, r protocol.StackPayload) (any, error) {
			return manager().Get(ctx, r.Name)
		}),
		protocol.ActionDockerStackSave: dockerCall(c, func(ctx context.Context, r protocol.StackSavePayload) (any, error) {
			return nil, manager().Save(ctx, r)
		}),
		protocol.ActionDockerStackUp:   stackAction(Stacks.Up),
		protocol.ActionDockerStackDown: stackAction(Stacks.Down),
		protocol.ActionDockerStackPull: stackAction(Stacks.Pull),
	}
}

// dockerList is the handler for a Docker action that takes no payload.
func dockerList(c *connection, list func(ctx context.Context) (any, error)) handler {
	return func(ctx context.Context, _ protocol.Message) (any, *protocol.Error) {
		if refusal := c.refuseUnlessAllowed(policy.Docker, "Docker"); refusal != nil {
			return nil, refusal
		}
		result, err := list(ctx)
		if err != nil {
			return nil, c.dockerError(err)
		}
		return result, nil
	}
}

// dockerCall is the handler for a Docker action with a payload of type P.
func dockerCall[P any](c *connection, run func(ctx context.Context, payload P) (any, error)) handler {
	return func(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
		if refusal := c.refuseUnlessAllowed(policy.Docker, "Docker"); refusal != nil {
			return nil, refusal
		}
		var payload P
		if err := protocol.DecodePayload(request, &payload); err != nil {
			return nil, invalidRequest(err)
		}
		result, err := run(ctx, payload)
		if err != nil {
			return nil, c.dockerError(err)
		}
		return result, nil
	}
}

func (c *connection) dockerError(err error) *protocol.Error {
	switch {
	case errors.Is(err, docker.ErrUnavailable), errors.Is(err, stacks.ErrUnavailable):
		return &protocol.Error{Code: protocol.ErrorDockerUnavailable, Message: "Docker wasn’t found on this machine: " + err.Error()}
	case errors.Is(err, docker.ErrNotFound), errors.Is(err, stacks.ErrNotFound):
		return &protocol.Error{Code: protocol.ErrorNotFound, Message: err.Error()}
	case errors.Is(err, docker.ErrInvalid), errors.Is(err, stacks.ErrInvalid):
		return &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: err.Error()}
	case errors.Is(err, docker.ErrConflict), errors.Is(err, stacks.ErrReadOnly):
		return &protocol.Error{Code: protocol.ErrorConflict, Message: err.Error()}
	default:
		c.agent.options.Logger.Warn("a Docker request failed", "error", err)
		return &protocol.Error{Code: protocol.ErrorInternal, Message: "Docker failed: " + err.Error()}
	}
}

// execInContainer opens a terminal on a shell in a container. It is a
// terminal session like any other, so input, resizes, closing and resuming
// use the terminal's stream handling.
func (c *connection) execInContainer(request protocol.ContainerExecPayload) (any, error) {
	start, err := c.agent.options.Docker.ExecStarter(request.ID, request.Command)
	if err != nil {
		return nil, err
	}
	open := protocol.TerminalOpenPayload{StreamID: request.StreamID, Cols: request.Cols, Rows: request.Rows,
		ResumeSessionID: request.ResumeSessionID}
	opened, err := c.agent.terminals.OpenWith(open, streamOutput{conn: c}, start)
	if errors.Is(err, terminal.ErrSessionEnded) {
		return nil, fmt.Errorf("%w: that terminal session has ended", docker.ErrNotFound)
	}
	return opened, err
}

// containerLogs returns a container's recent lines and, if the request
// names a stream, then follows the log on it, one JSON line per frame.
func (c *connection) containerLogs(ctx context.Context, request protocol.ContainerLogsPayload) (any, error) {
	recent, err := c.agent.options.Docker.Logs(ctx, request)
	if err != nil || request.StreamID == 0 {
		return recent, err
	}
	// Following starts just after the last line sent, or now.
	since := c.agent.options.Clock.Now().UnixMilli()
	if count := len(recent.Lines); count > 0 && recent.Lines[count-1].Time > 0 {
		since = recent.Lines[count-1].Time + 1
	}
	out := streamOutput{conn: c}
	c.follow(ctx, request.StreamID, func(ctx context.Context) string {
		err := c.agent.options.Docker.Follow(ctx, request.ID, since, func(line protocol.ContainerLogLine) {
			data, _ := json.Marshal(line)                                //nolint:errchkjson // a struct of strings and numbers always encodes
			_ = out.SendFrame(ctx, request.StreamID, append(data, '\n')) // a gone viewer ends the follow through ctx
		})
		if err != nil {
			return err.Error()
		}
		return "the container stopped"
	})
	return recent, nil
}

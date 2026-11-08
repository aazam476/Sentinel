// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"errors"

	"github.com/aazam476/sentinel/internal/agent/docker"
	"github.com/aazam476/sentinel/internal/agent/policy"
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
		protocol.ActionDockerContainerLogs: dockerCall(c, func(ctx context.Context, r protocol.ContainerLogsPayload) (any, error) {
			return engine().Logs(ctx, r)
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
	case errors.Is(err, docker.ErrUnavailable):
		return &protocol.Error{Code: protocol.ErrorDockerUnavailable, Message: "Docker wasn’t found on this machine: " + err.Error()}
	case errors.Is(err, docker.ErrNotFound):
		return &protocol.Error{Code: protocol.ErrorNotFound, Message: err.Error()}
	case errors.Is(err, docker.ErrInvalid):
		return &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: err.Error()}
	case errors.Is(err, docker.ErrConflict):
		return &protocol.Error{Code: protocol.ErrorConflict, Message: err.Error()}
	default:
		c.agent.options.Logger.Warn("a Docker request failed", "error", err)
		return &protocol.Error{Code: protocol.ErrorInternal, Message: "Docker failed: " + err.Error()}
	}
}

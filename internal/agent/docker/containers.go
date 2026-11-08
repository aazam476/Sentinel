// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/aazam476/sentinel/internal/protocol"
)

// statsWorkers is how many containers' statistics are read at once. Each
// takes the engine about a second, to measure CPU use between two samples.
const statsWorkers = 8

// Compose labels a container with its project and service.
const (
	labelProject = "com.docker.compose.project"
	labelService = "com.docker.compose.service"
)

// containerRef matches container IDs and names. Names may start with "/"
// in Docker's own lists, but not in requests.
var containerRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)

// checkContainer validates a container ID or name from the server.
func checkContainer(ref string) error {
	if !containerRef.MatchString(ref) {
		return fmt.Errorf("%w: container %q", ErrInvalid, ref)
	}
	return nil
}

// Containers lists every container, with CPU and memory use for the
// running ones.
func (e *Engine) Containers(ctx context.Context) (protocol.ContainerListPayload, error) {
	var listed []container.Summary
	err := e.call(ctx, func(c *client.Client) error {
		result, err := c.ContainerList(ctx, client.ContainerListOptions{All: true})
		listed = result.Items
		return err
	})
	if err != nil {
		return protocol.ContainerListPayload{}, fmt.Errorf("list containers: %w", err)
	}
	containers := make([]protocol.Container, len(listed))
	for i, summary := range listed {
		containers[i] = fromSummary(summary)
	}
	e.addStats(ctx, containers)
	engine, err := e.Describe(ctx)
	if err != nil {
		return protocol.ContainerListPayload{}, err
	}
	return protocol.ContainerListPayload{Engine: engine, Containers: containers}, nil
}

func fromSummary(summary container.Summary) protocol.Container {
	result := protocol.Container{
		ID: summary.ID, Image: summary.Image, State: string(summary.State), Status: summary.Status,
		Created: summary.Created * 1000, Ports: []protocol.ContainerPort{},
		Stack: summary.Labels[labelProject], Service: summary.Labels[labelService],
	}
	if len(summary.Names) > 0 {
		result.Name = strings.TrimPrefix(summary.Names[0], "/")
	}
	for _, port := range summary.Ports {
		published := protocol.ContainerPort{PrivatePort: port.PrivatePort, PublicPort: port.PublicPort, Protocol: port.Type}
		if port.IP.IsValid() {
			published.IP = port.IP.String()
		}
		result.Ports = append(result.Ports, published)
	}
	return result
}

// addStats fills in CPU and memory use for running containers, a few at a
// time. A container whose statistics can't be read keeps zeros.
func (e *Engine) addStats(ctx context.Context, containers []protocol.Container) {
	work := make(chan *protocol.Container)
	var workers sync.WaitGroup
	for range statsWorkers {
		workers.Go(func() {
			for target := range work {
				if stats, err := e.stats(ctx, target.ID); err == nil {
					target.CPUPercent, target.MemoryBytes, target.MemoryLimitBytes = usage(stats)
				}
			}
		})
	}
	for i := range containers {
		if containers[i].State == string(container.StateRunning) {
			work <- &containers[i]
		}
	}
	close(work)
	workers.Wait()
}

func (e *Engine) stats(ctx context.Context, id string) (container.StatsResponse, error) {
	var stats container.StatsResponse
	err := e.call(ctx, func(c *client.Client) error {
		result, err := c.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: false})
		if err != nil {
			return err
		}
		defer result.Body.Close() //nolint:errcheck // read-only body
		return json.NewDecoder(result.Body).Decode(&stats)
	})
	return stats, err
}

// usage computes CPU use between the two samples in stats (100% is one
// core, as `docker stats` shows it) and memory in use, not counting the
// page cache the kernel can drop at any time.
func usage(stats container.StatsResponse) (cpuPercent float64, memory, limit uint64) {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	if cpuDelta > 0 && systemDelta > 0 {
		cpus := float64(max(stats.CPUStats.OnlineCPUs, 1))
		cpuPercent = cpuDelta / systemDelta * cpus * 100
	}
	memory = stats.MemoryStats.Usage
	cache := stats.MemoryStats.Stats["inactive_file"] // cgroup v2
	if cache == 0 {
		cache = stats.MemoryStats.Stats["total_inactive_file"] // cgroup v1
	}
	if cache < memory {
		memory -= cache
	}
	if stats.MemoryStats.PrivateWorkingSet > 0 { // Windows
		memory = stats.MemoryStats.PrivateWorkingSet
	}
	return cpuPercent, memory, stats.MemoryStats.Limit
}

// ContainerAction is start, stop, restart or remove.
type ContainerAction string

// The container actions.
const (
	Start   ContainerAction = "start"
	Stop    ContainerAction = "stop"
	Restart ContainerAction = "restart"
	Remove  ContainerAction = "remove"
)

// DoContainer starts, stops, restarts or removes a container.
func (e *Engine) DoContainer(ctx context.Context, action ContainerAction, request protocol.ContainerPayload) error {
	if err := checkContainer(request.ID); err != nil {
		return err
	}
	err := e.call(ctx, func(c *client.Client) error {
		var err error
		switch action {
		case Start:
			_, err = c.ContainerStart(ctx, request.ID, client.ContainerStartOptions{})
		case Stop:
			_, err = c.ContainerStop(ctx, request.ID, client.ContainerStopOptions{})
		case Restart:
			_, err = c.ContainerRestart(ctx, request.ID, client.ContainerRestartOptions{})
		case Remove:
			_, err = c.ContainerRemove(ctx, request.ID, client.ContainerRemoveOptions{Force: request.Force})
		default:
			err = fmt.Errorf("%w: container action %q", ErrInvalid, action)
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("%s container %s: %w", action, request.ID, err)
	}
	return nil
}

// Inspect returns Docker's description of a container, as Docker wrote it.
func (e *Engine) Inspect(ctx context.Context, id string) (protocol.ContainerInspectPayload, error) {
	if err := checkContainer(id); err != nil {
		return protocol.ContainerInspectPayload{}, err
	}
	var raw json.RawMessage
	err := e.call(ctx, func(c *client.Client) error {
		result, err := c.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		raw = result.Raw
		return err
	})
	if err != nil {
		return protocol.ContainerInspectPayload{}, fmt.Errorf("inspect container %s: %w", id, err)
	}
	return protocol.ContainerInspectPayload{Inspect: raw}, nil
}

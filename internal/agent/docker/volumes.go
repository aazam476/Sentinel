// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/aazam476/sentinel/internal/protocol"
)

// objectName matches volume and network names and IDs.
var objectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)

// builtInNetworks exist on every engine and can't be removed.
func builtInNetworks() []string { return []string{"bridge", "host", "none", "podman"} }

func checkName(kind, name string) error {
	if !objectName.MatchString(name) {
		return fmt.Errorf("%w: %s %q", ErrInvalid, kind, name)
	}
	return nil
}

// Volumes lists the volumes, marking those a container uses.
func (e *Engine) Volumes(ctx context.Context) (protocol.VolumeListPayload, error) {
	volumes := []protocol.Volume{}
	err := e.call(ctx, func(c *client.Client) error {
		listed, err := c.VolumeList(ctx, client.VolumeListOptions{})
		if err != nil {
			return err
		}
		containers, err := c.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err != nil {
			return err
		}
		used := usedVolumes(containers.Items)
		for _, volume := range listed.Items {
			created, _ := time.Parse(time.RFC3339, volume.CreatedAt) // zero when missing
			volumes = append(volumes, protocol.Volume{Name: volume.Name, Driver: volume.Driver,
				Mountpoint: volume.Mountpoint, InUse: used[volume.Name], Stack: volume.Labels[labelProject],
				Created: max(created.UnixMilli(), 0)})
		}
		return nil
	})
	if err != nil {
		return protocol.VolumeListPayload{}, fmt.Errorf("list volumes: %w", err)
	}
	return protocol.VolumeListPayload{Volumes: volumes}, nil
}

func usedVolumes(containers []container.Summary) map[string]bool {
	used := map[string]bool{}
	for _, summary := range containers {
		for _, mount := range summary.Mounts {
			if mount.Name != "" {
				used[mount.Name] = true
			}
		}
	}
	return used
}

// RemoveVolume removes a volume no container uses; Docker refuses one in use.
func (e *Engine) RemoveVolume(ctx context.Context, name string) error {
	if err := checkName("volume", name); err != nil {
		return err
	}
	err := e.call(ctx, func(c *client.Client) error {
		_, err := c.VolumeRemove(ctx, name, client.VolumeRemoveOptions{})
		return err
	})
	if err != nil {
		return fmt.Errorf("remove volume %s: %w", name, err)
	}
	return nil
}

// Networks lists the networks, with how many containers each connects.
func (e *Engine) Networks(ctx context.Context) (protocol.NetworkListPayload, error) {
	networks := []protocol.Network{}
	err := e.call(ctx, func(c *client.Client) error {
		listed, err := c.NetworkList(ctx, client.NetworkListOptions{})
		if err != nil {
			return err
		}
		containers, err := c.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err != nil {
			return err
		}
		attached := map[string]int{}
		for _, summary := range containers.Items {
			if summary.NetworkSettings == nil {
				continue
			}
			for _, endpoint := range summary.NetworkSettings.Networks {
				if endpoint != nil {
					attached[endpoint.NetworkID]++
				}
			}
		}
		for _, network := range listed.Items {
			networks = append(networks, protocol.Network{ID: network.ID, Name: network.Name, Driver: network.Driver,
				Scope: network.Scope, Containers: attached[network.ID], Stack: network.Labels[labelProject],
				BuiltIn: slices.Contains(builtInNetworks(), network.Name)})
		}
		return nil
	})
	if err != nil {
		return protocol.NetworkListPayload{}, fmt.Errorf("list networks: %w", err)
	}
	return protocol.NetworkListPayload{Networks: networks}, nil
}

// RemoveNetwork removes a network no container uses. The built-in ones are
// refused here rather than by Docker, with a clearer message.
func (e *Engine) RemoveNetwork(ctx context.Context, name string) error {
	if err := checkName("network", name); err != nil {
		return err
	}
	if slices.Contains(builtInNetworks(), name) {
		return fmt.Errorf("%w: the %s network is built in", ErrConflict, name)
	}
	err := e.call(ctx, func(c *client.Client) error {
		_, err := c.NetworkRemove(ctx, name, client.NetworkRemoveOptions{})
		return err
	})
	if err != nil {
		return fmt.Errorf("remove network %s: %w", name, err)
	}
	return nil
}

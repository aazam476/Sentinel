// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"

	"github.com/aazam476/sentinel/internal/protocol"
)

// imageID matches image IDs, full (sha256:...) or short.
var imageID = regexp.MustCompile(`^(sha256:)?[a-f0-9]{12,64}$`)

// checkImageRef validates an image reference (name with tag or digest).
func checkImageRef(ref string) (reference.Named, error) {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil || strings.HasPrefix(ref, "-") {
		return nil, fmt.Errorf("%w: image %q", ErrInvalid, ref)
	}
	return reference.TagNameOnly(named), nil
}

// Images lists the images, with how many containers use each.
func (e *Engine) Images(ctx context.Context) (protocol.ImageListPayload, error) {
	var listed []protocol.Image
	err := e.call(ctx, func(c *client.Client) error {
		images, err := c.ImageList(ctx, client.ImageListOptions{})
		if err != nil {
			return err
		}
		containers, err := c.ContainerList(ctx, client.ContainerListOptions{All: true})
		if err != nil {
			return err
		}
		users := map[string]int{}
		for _, used := range containers.Items {
			users[used.ImageID]++
		}
		for _, image := range images.Items {
			listed = append(listed, protocol.Image{ID: image.ID, Tags: nonNil(withoutNone(image.RepoTags)),
				Digests: nonNil(withoutNone(image.RepoDigests)), SizeBytes: image.Size,
				Created: image.Created * 1000, Containers: users[image.ID]})
		}
		return nil
	})
	if err != nil {
		return protocol.ImageListPayload{}, fmt.Errorf("list images: %w", err)
	}
	return protocol.ImageListPayload{Images: nonNil(listed)}, nil
}

// withoutNone drops the "<none>:<none>" placeholders of untagged images.
func withoutNone(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return strings.HasPrefix(name, "<none>") })
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// encodeAuth turns a registry login into the header Docker expects. The
// login is only ever in memory (§9.4).
func encodeAuth(auth *protocol.RegistryAuth) (string, error) {
	if auth == nil {
		return "", nil
	}
	encoded, err := authconfig.Encode(registry.AuthConfig{Username: auth.Username, Password: auth.Password,
		ServerAddress: auth.ServerAddress})
	if err != nil {
		return "", fmt.Errorf("encode registry login: %w", err)
	}
	return encoded, nil
}

// Pull pulls an image and returns the digest it got.
func (e *Engine) Pull(ctx context.Context, request protocol.ImageRefPayload) (protocol.ImagePulledPayload, error) {
	named, err := checkImageRef(request.Reference)
	if err != nil {
		return protocol.ImagePulledPayload{}, err
	}
	auth, err := encodeAuth(request.Auth)
	if err != nil {
		return protocol.ImagePulledPayload{}, err
	}
	var digest string
	err = e.call(ctx, func(c *client.Client) error {
		pull, err := c.ImagePull(ctx, named.String(), client.ImagePullOptions{RegistryAuth: auth})
		if err != nil {
			return err
		}
		defer pull.Close() //nolint:errcheck // read-only body
		for message, err := range pull.JSONMessages(ctx) {
			if err != nil {
				return err
			}
			if message.Error != nil {
				return fmt.Errorf("%s", message.Error.Message)
			}
			if found, ok := strings.CutPrefix(message.Status, "Digest: "); ok {
				digest = found
			}
		}
		return nil
	})
	if err != nil {
		return protocol.ImagePulledPayload{}, fmt.Errorf("pull %s: %w", named, err)
	}
	return protocol.ImagePulledPayload{Digest: digest}, nil
}

// RemoveImage removes an image by ID or reference.
func (e *Engine) RemoveImage(ctx context.Context, request protocol.ImageRefPayload) error {
	ref := request.Reference
	if !imageID.MatchString(ref) {
		named, err := checkImageRef(ref)
		if err != nil {
			return err
		}
		ref = named.String()
	}
	err := e.call(ctx, func(c *client.Client) error {
		_, err := c.ImageRemove(ctx, ref, client.ImageRemoveOptions{Force: request.Force, PruneChildren: true})
		return err
	})
	if err != nil {
		return fmt.Errorf("remove image %s: %w", ref, err)
	}
	return nil
}

// CheckImage asks the image's registry for the digest its tag points to
// now, and compares it with the machine's copy.
func (e *Engine) CheckImage(ctx context.Context, request protocol.ImageRefPayload) (protocol.ImageCheckPayload, error) {
	named, err := checkImageRef(request.Reference)
	if err != nil {
		return protocol.ImageCheckPayload{}, err
	}
	auth, err := encodeAuth(request.Auth)
	if err != nil {
		return protocol.ImageCheckPayload{}, err
	}
	result := protocol.ImageCheckPayload{LocalDigests: []string{}}
	err = e.call(ctx, func(c *client.Client) error {
		local, err := c.ImageInspect(ctx, named.String())
		if err != nil {
			return err
		}
		remote, err := c.DistributionInspect(ctx, named.String(), client.DistributionInspectOptions{EncodedRegistryAuth: auth})
		if err != nil {
			return err
		}
		result.RegistryDigest = remote.Descriptor.Digest.String()
		for _, repoDigest := range local.RepoDigests {
			_, digest, _ := strings.Cut(repoDigest, "@")
			result.LocalDigests = append(result.LocalDigests, digest)
		}
		result.UpdateAvailable = !slices.Contains(result.LocalDigests, result.RegistryDigest)
		return nil
	})
	if err != nil {
		return protocol.ImageCheckPayload{}, fmt.Errorf("check %s for updates: %w", named, err)
	}
	return result, nil
}

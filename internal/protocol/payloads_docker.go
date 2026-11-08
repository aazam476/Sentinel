// SPDX-License-Identifier: Apache-2.0

package protocol

import "encoding/json"

// Docker actions (§9.4).
const (
	ActionDockerContainerList    = "docker.container.list"
	ActionDockerContainerStart   = "docker.container.start"
	ActionDockerContainerStop    = "docker.container.stop"
	ActionDockerContainerRestart = "docker.container.restart"
	ActionDockerContainerRemove  = "docker.container.remove"
	ActionDockerContainerInspect = "docker.container.inspect"
	ActionDockerContainerLogs    = "docker.container.logs"
	ActionDockerContainerExec    = "docker.container.exec"

	ActionDockerImageList   = "docker.image.list"
	ActionDockerImagePull   = "docker.image.pull"
	ActionDockerImageRemove = "docker.image.remove"
	ActionDockerImageCheck  = "docker.image.check"

	ActionDockerVolumeList    = "docker.volume.list"
	ActionDockerVolumeRemove  = "docker.volume.remove"
	ActionDockerNetworkList   = "docker.network.list"
	ActionDockerNetworkRemove = "docker.network.remove"

	ActionDockerStackList = "docker.stack.list"
	ActionDockerStackGet  = "docker.stack.get"
	ActionDockerStackSave = "docker.stack.save"
	ActionDockerStackUp   = "docker.stack.up"
	ActionDockerStackDown = "docker.stack.down"
	ActionDockerStackPull = "docker.stack.pull"
)

// ContainerPort is one published or exposed port.
type ContainerPort struct {
	// IP is the host address it is published on, or empty if unpublished.
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"privatePort"`
	// PublicPort is 0 when the port isn't published on the host.
	PublicPort uint16 `json:"publicPort,omitempty"`
	Protocol   string `json:"protocol"`
}

// Container is one container on the machine.
type Container struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	// State is created, running, paused, restarting, removing, exited or dead.
	State string `json:"state"`
	// Status is Docker's own summary, such as "Up 3 hours (healthy)".
	Status string `json:"status"`
	// Created is when it was created, UTC Unix milliseconds.
	Created int64           `json:"created"`
	Ports   []ContainerPort `json:"ports"`
	// Stack and Service name its compose project and service, if any.
	Stack   string `json:"stack,omitempty"`
	Service string `json:"service,omitempty"`
	// CPUPercent, MemoryBytes and MemoryLimitBytes are set for running
	// containers. 100% CPU is one whole core.
	CPUPercent       float64 `json:"cpuPercent"`
	MemoryBytes      uint64  `json:"memoryBytes"`
	MemoryLimitBytes uint64  `json:"memoryLimitBytes"`
}

// DockerEngine describes the container engine the agent talks to.
type DockerEngine struct {
	// Name is "Docker" or "Podman".
	Name    string `json:"name"`
	Version string `json:"version"`
	Socket  string `json:"socket"`
}

// ContainerListPayload answers docker.container.list.
type ContainerListPayload struct {
	Engine     DockerEngine `json:"engine"`
	Containers []Container  `json:"containers"`
}

// ContainerPayload names a container for start, stop, restart, remove and
// inspect, by ID or name.
type ContainerPayload struct {
	ID string `json:"id"`
	// Force removes a running container (remove only).
	Force bool `json:"force,omitempty"`
}

// ContainerInspectPayload answers docker.container.inspect with Docker's
// own description of the container, as JSON.
type ContainerInspectPayload struct {
	Inspect json.RawMessage `json:"inspect"`
}

// ContainerLogsPayload asks for a container's recent log lines.
type ContainerLogsPayload struct {
	ID string `json:"id"`
	// Tail is how many of the last lines to return: 1 to 5,000, default 500.
	Tail int `json:"tail,omitempty"`
	// Since keeps lines from this time on, UTC Unix milliseconds; 0 for all.
	Since int64 `json:"since,omitempty"`
	// StreamID, if set, follows the log on that stream (§7.6) after
	// sending the recent lines, until the stream is closed.
	StreamID StreamID `json:"streamId,omitempty"`
}

// ContainerLogLine is one line a container wrote.
type ContainerLogLine struct {
	// Time is UTC Unix milliseconds.
	Time int64 `json:"time"`
	// Stream is stdout or stderr.
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

// ContainerLogsResultPayload answers docker.container.logs.
type ContainerLogsResultPayload struct {
	Lines []ContainerLogLine `json:"lines"`
}

// ContainerExecPayload starts a shell in a container with a terminal,
// carried on the stream like the machine's own terminal.
type ContainerExecPayload struct {
	ID       string   `json:"id"`
	StreamID StreamID `json:"streamId"`
	Cols     uint16   `json:"cols"`
	Rows     uint16   `json:"rows"`
	// Command is the program to run, default a shell found in the image.
	Command []string `json:"command,omitempty"`
}

// Image is one image on the machine.
type Image struct {
	ID string `json:"id"`
	// Tags are its names, such as nginx:1.27; empty for a dangling image.
	Tags []string `json:"tags"`
	// Digests are its registry digests, such as nginx@sha256:....
	Digests   []string `json:"digests"`
	SizeBytes int64    `json:"sizeBytes"`
	// Created is when it was built, UTC Unix milliseconds.
	Created int64 `json:"created"`
	// Containers counts the containers using it.
	Containers int `json:"containers"`
}

// ImageListPayload answers docker.image.list.
type ImageListPayload struct {
	Images []Image `json:"images"`
}

// RegistryAuth is a registry login, sent by the server for one operation
// and never written to disk by the agent (§9.4).
type RegistryAuth struct {
	ServerAddress string `json:"serverAddress"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

// ImageRefPayload names an image for pull, remove and check.
type ImageRefPayload struct {
	// Reference is an image name with tag or digest, or (remove only) an ID.
	Reference string `json:"reference"`
	// Auth logs in to the image's registry, if it needs it.
	Auth *RegistryAuth `json:"auth,omitempty"`
	// Force removes an image even if a stopped container uses it.
	Force bool `json:"force,omitempty"`
}

// ImagePulledPayload answers docker.image.pull.
type ImagePulledPayload struct {
	// Digest is the digest pulled, such as sha256:....
	Digest string `json:"digest"`
}

// ImageCheckPayload answers docker.image.check: whether the registry has a
// newer image for the reference than the one on the machine.
type ImageCheckPayload struct {
	LocalDigests    []string `json:"localDigests"`
	RegistryDigest  string   `json:"registryDigest"`
	UpdateAvailable bool     `json:"updateAvailable"`
}

// Volume is one volume.
type Volume struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	// Mountpoint is where it lives on the machine.
	Mountpoint string `json:"mountpoint"`
	// Created is UTC Unix milliseconds, or 0 if unknown.
	Created int64 `json:"created"`
	// InUse is true when a container uses it.
	InUse bool `json:"inUse"`
	// Stack names the compose project that created it, if any.
	Stack string `json:"stack,omitempty"`
}

// VolumeListPayload answers docker.volume.list.
type VolumeListPayload struct {
	Volumes []Volume `json:"volumes"`
}

// Network is one network.
type Network struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Scope  string `json:"scope"`
	// Containers counts the containers attached.
	Containers int `json:"containers"`
	// BuiltIn is true for bridge, host and none, which can't be removed.
	BuiltIn bool   `json:"builtIn"`
	Stack   string `json:"stack,omitempty"`
}

// NetworkListPayload answers docker.network.list.
type NetworkListPayload struct {
	Networks []Network `json:"networks"`
}

// NamePayload names a volume or network to remove.
type NamePayload struct {
	Name string `json:"name"`
}

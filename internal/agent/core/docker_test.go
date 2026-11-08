// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/docker"
	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/agent/stacks"
	"github.com/aazam476/sentinel/internal/agent/terminal"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// fakeDocker answers every Docker call with err, or with a small result,
// and records what it was asked to do.
type fakeDocker struct {
	err  error
	done chan string
	// followEnds makes a followed log end by itself, as when the
	// container stops; otherwise it runs until the stream is closed.
	followEnds bool
	followed   chan struct{}
}

func (d *fakeDocker) record(what string) error {
	d.done <- what
	return d.err
}

func (d *fakeDocker) Containers(context.Context) (protocol.ContainerListPayload, error) {
	return protocol.ContainerListPayload{Containers: []protocol.Container{{Name: "web"}}}, d.record("containers")
}

func (d *fakeDocker) DoContainer(_ context.Context, action docker.ContainerAction, r protocol.ContainerPayload) error {
	return d.record(string(action) + " " + r.ID)
}

func (d *fakeDocker) Inspect(_ context.Context, id string) (protocol.ContainerInspectPayload, error) {
	return protocol.ContainerInspectPayload{Inspect: []byte(`{}`)}, d.record("inspect " + id)
}

func (d *fakeDocker) Logs(_ context.Context, r protocol.ContainerLogsPayload) (protocol.ContainerLogsResultPayload, error) {
	return protocol.ContainerLogsResultPayload{}, d.record("logs " + r.ID)
}

func (d *fakeDocker) Images(context.Context) (protocol.ImageListPayload, error) {
	return protocol.ImageListPayload{}, d.record("images")
}

func (d *fakeDocker) Pull(_ context.Context, r protocol.ImageRefPayload) (protocol.ImagePulledPayload, error) {
	return protocol.ImagePulledPayload{}, d.record("pull " + r.Reference)
}

func (d *fakeDocker) RemoveImage(_ context.Context, r protocol.ImageRefPayload) error {
	return d.record("remove image " + r.Reference)
}

func (d *fakeDocker) CheckImage(_ context.Context, r protocol.ImageRefPayload) (protocol.ImageCheckPayload, error) {
	return protocol.ImageCheckPayload{}, d.record("check " + r.Reference)
}

func (d *fakeDocker) Volumes(context.Context) (protocol.VolumeListPayload, error) {
	return protocol.VolumeListPayload{}, d.record("volumes")
}

func (d *fakeDocker) RemoveVolume(_ context.Context, name string) error {
	return d.record("remove volume " + name)
}

func (d *fakeDocker) Networks(context.Context) (protocol.NetworkListPayload, error) {
	return protocol.NetworkListPayload{}, d.record("networks")
}

func (d *fakeDocker) RemoveNetwork(_ context.Context, name string) error {
	return d.record("remove network " + name)
}

func (d *fakeDocker) Follow(ctx context.Context, id string, _ int64, emit func(protocol.ContainerLogLine)) error {
	emit(protocol.ContainerLogLine{Time: 1, Stream: "stdout", Text: "followed " + id})
	if !d.followEnds {
		<-ctx.Done()
	}
	close(d.followed)
	return nil
}

func (d *fakeDocker) ExecStarter(id string, command []string) (terminal.Starter, error) {
	if err := d.record("exec " + id + " " + strings.Join(command, " ")); err != nil {
		return nil, err
	}
	return func(uint16, uint16) (terminal.Shell, error) { return newEchoShell(), nil }, nil
}

// fakeStacks is the stacks side of a fakeDocker, recording into it.
type fakeStacks struct{ *fakeDocker }

func (s fakeStacks) List(context.Context) (protocol.StackListPayload, error) {
	return protocol.StackListPayload{}, s.record("stacks")
}

func (s fakeStacks) Get(_ context.Context, name string) (protocol.StackFilesPayload, error) {
	return protocol.StackFilesPayload{}, s.record("get " + name)
}

func (s fakeStacks) Save(_ context.Context, r protocol.StackSavePayload) error {
	return s.record("save " + r.Name)
}

func (s fakeStacks) Up(_ context.Context, name string) error   { return s.record("up " + name) }
func (s fakeStacks) Down(_ context.Context, name string) error { return s.record("down " + name) }
func (s fakeStacks) Pull(_ context.Context, name string) error { return s.record("pull stack " + name) }

func startWithDocker(t *testing.T, fake *fakeDocker, localPolicy policy.Policy) (*server, string) {
	t.Helper()
	s := newServer(t)
	s.machine.Docker = fake
	s.machine.Stacks = fakeStacks{fake}
	events := s.subscribe()
	agent := s.startAgent(localPolicy)
	waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
	return s, agent.state.AgentID
}

func TestDockerRequests(t *testing.T) {
	t.Run("answers every Docker action", func(t *testing.T) {
		fake := &fakeDocker{done: make(chan string, 1)}
		s, agentID := startWithDocker(t, fake, policy.Policy{})
		actions := []struct {
			action  string
			payload any
			did     string
		}{
			{protocol.ActionDockerContainerList, nil, "containers"},
			{protocol.ActionDockerContainerStart, protocol.ContainerPayload{ID: "web"}, "start web"},
			{protocol.ActionDockerContainerStop, protocol.ContainerPayload{ID: "web"}, "stop web"},
			{protocol.ActionDockerContainerRestart, protocol.ContainerPayload{ID: "web"}, "restart web"},
			{protocol.ActionDockerContainerRemove, protocol.ContainerPayload{ID: "web"}, "remove web"},
			{protocol.ActionDockerContainerInspect, protocol.ContainerPayload{ID: "web"}, "inspect web"},
			{protocol.ActionDockerContainerLogs, protocol.ContainerLogsPayload{ID: "web"}, "logs web"},
			{protocol.ActionDockerImageList, nil, "images"},
			{protocol.ActionDockerImagePull, protocol.ImageRefPayload{Reference: "nginx"}, "pull nginx"},
			{protocol.ActionDockerImageRemove, protocol.ImageRefPayload{Reference: "nginx"}, "remove image nginx"},
			{protocol.ActionDockerImageCheck, protocol.ImageRefPayload{Reference: "nginx"}, "check nginx"},
			{protocol.ActionDockerVolumeList, nil, "volumes"},
			{protocol.ActionDockerVolumeRemove, protocol.NamePayload{Name: "data"}, "remove volume data"},
			{protocol.ActionDockerNetworkList, nil, "networks"},
			{protocol.ActionDockerNetworkRemove, protocol.NamePayload{Name: "net"}, "remove network net"},
			{protocol.ActionDockerStackList, nil, "stacks"},
			{protocol.ActionDockerStackGet, protocol.StackPayload{Name: "blog"}, "get blog"},
			{protocol.ActionDockerStackSave, protocol.StackSavePayload{Name: "blog"}, "save blog"},
			{protocol.ActionDockerStackUp, protocol.StackPayload{Name: "blog"}, "up blog"},
			{protocol.ActionDockerStackDown, protocol.StackPayload{Name: "blog"}, "down blog"},
			{protocol.ActionDockerStackPull, protocol.StackPayload{Name: "blog"}, "pull stack blog"},
		}
		for _, a := range actions {
			if err := s.request(t, agentID, a.action, a.payload, nil); err != nil {
				t.Errorf("%s: %v", a.action, err)
			}
			if did := <-fake.done; did != a.did {
				t.Errorf("%s did %q, want %q", a.action, did, a.did)
			}
		}
	})

	t.Run("SEC-22: refuses every Docker action when the local policy switches Docker off", func(t *testing.T) {
		localPolicy, err := policy.Parse("[capabilities]\ndocker = false\n")
		if err != nil {
			t.Fatal(err)
		}
		fake := &fakeDocker{done: make(chan string, 1)}
		s, agentID := startWithDocker(t, fake, localPolicy)
		for _, action := range []string{protocol.ActionDockerContainerList, protocol.ActionDockerContainerRestart} {
			var agentErr *protocol.Error
			if err := s.request(t, agentID, action, protocol.ContainerPayload{ID: "web"}, nil); !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorPolicyDenied {
				t.Errorf("%s: err = %v", action, err)
			}
		}
		select {
		case did := <-fake.done:
			t.Errorf("Docker was asked to %s", did)
		default:
		}
	})

	t.Run("follows a container's log on a stream until the server closes it", func(t *testing.T) {
		for _, ends := range []bool{false, true} {
			fake := &fakeDocker{done: make(chan string, 1), followEnds: ends, followed: make(chan struct{})}
			s, agentID := startWithDocker(t, fake, policy.Policy{})
			stored, err := s.fleet.AgentForHandshake(t.Context(), agentID)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := s.hub.OpenStream(t.Context(), stored.ID, protocol.ActionDockerContainerLogs, func(id protocol.StreamID) any {
				return protocol.ContainerLogsPayload{ID: "web", StreamID: id}
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			<-fake.done
			if got := string(<-stream.Frames()); got != `{"time":1,"stream":"stdout","text":"followed web"}`+"\n" {
				t.Errorf("frame = %q", got)
			}
			if ends {
				if reason := stream.Reason(); reason != "the container stopped" {
					t.Errorf("reason = %q", reason)
				}
			} else {
				stream.Close(t.Context(), "closed by the user")
			}
			<-fake.followed
		}
	})

	t.Run("runs a shell in a container as a terminal session", func(t *testing.T) {
		fake := &fakeDocker{done: make(chan string, 1)}
		s, agentID := startWithDocker(t, fake, policy.Policy{})
		stored, err := s.fleet.AgentForHandshake(t.Context(), agentID)
		if err != nil {
			t.Fatal(err)
		}
		var opened protocol.TerminalOpenedPayload
		stream, err := s.hub.OpenStream(t.Context(), stored.ID, protocol.ActionDockerContainerExec, func(id protocol.StreamID) any {
			return protocol.ContainerExecPayload{ID: "web", StreamID: id, Cols: 80, Rows: 24}
		}, &opened)
		if err != nil || opened.SessionID == "" {
			t.Fatalf("exec = %+v (%v)", opened, err)
		}
		if did := <-fake.done; did != "exec web " {
			t.Errorf("did %q", did)
		}
		if err := stream.Write(t.Context(), []byte("ls\r")); err != nil {
			t.Fatal(err)
		}
		if got := string(<-stream.Frames()); got != "you typed: ls\r" {
			t.Errorf("output = %q", got)
		}
		// A session that doesn't exist can't be resumed.
		_, err = s.hub.OpenStream(t.Context(), stored.ID, protocol.ActionDockerContainerExec, func(id protocol.StreamID) any {
			return protocol.ContainerExecPayload{ID: "web", StreamID: id, ResumeSessionID: "01JBX3K5Q7Z8M4N6P8R0T2V4W6"}
		}, nil)
		<-fake.done
		var agentErr *protocol.Error
		if !errors.As(err, &agentErr) || agentErr.Code != protocol.ErrorNotFound {
			t.Errorf("resume unknown: %v", err)
		}
	})

	cases := []struct {
		name    string
		action  string
		payload any
		err     error
		code    protocol.ErrorCode
	}{
		{"a shell that can't start in a container", protocol.ActionDockerContainerExec, protocol.ContainerExecPayload{ID: "web"}, docker.ErrNotFound, protocol.ErrorNotFound},
		{"no Docker on the machine", protocol.ActionDockerContainerList, nil, docker.ErrUnavailable, protocol.ErrorDockerUnavailable},
		{"a container that doesn't exist", protocol.ActionDockerContainerStop, protocol.ContainerPayload{ID: "x"}, docker.ErrNotFound, protocol.ErrorNotFound},
		{"an invalid name", protocol.ActionDockerVolumeRemove, protocol.NamePayload{Name: "x"}, docker.ErrInvalid, protocol.ErrorInvalidRequest},
		{"a volume in use", protocol.ActionDockerVolumeRemove, protocol.NamePayload{Name: "x"}, docker.ErrConflict, protocol.ErrorConflict},
		{"anything else", protocol.ActionDockerImagePull, protocol.ImageRefPayload{Reference: "x"}, errors.New("disk full"), protocol.ErrorInternal},
		{"a stack whose file can't be edited", protocol.ActionDockerStackSave, protocol.StackSavePayload{Name: "media"}, stacks.ErrReadOnly, protocol.ErrorConflict},
		{"no docker compose", protocol.ActionDockerStackList, nil, stacks.ErrUnavailable, protocol.ErrorDockerUnavailable},
		{"a malformed request", protocol.ActionDockerImagePull, "nginx", nil, protocol.ErrorInvalidRequest},
	}
	for _, c := range cases {
		t.Run("reports "+c.name, func(t *testing.T) {
			fake := &fakeDocker{err: c.err, done: make(chan string, 1)}
			s, agentID := startWithDocker(t, fake, policy.Policy{})
			var agentErr *protocol.Error
			if err := s.request(t, agentID, c.action, c.payload, nil); !errors.As(err, &agentErr) || agentErr.Code != c.code {
				t.Errorf("err = %v, want %s", err, c.code)
			}
		})
	}
}

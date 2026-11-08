// SPDX-License-Identifier: Apache-2.0

package docker_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/docker"
	"github.com/aazam476/sentinel/internal/protocol"
)

func milli(text string) int64 {
	at, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		panic(err)
	}
	return at.UnixMilli()
}

func TestContainers(t *testing.T) {
	t.Run("lists containers with ports, compose labels and the running ones' usage", func(t *testing.T) {
		list, err := newFakeEngine(t).engine().Containers(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if list.Engine.Name != "Docker" || list.Engine.Version != "29.9.2" {
			t.Errorf("engine = %+v", list.Engine)
		}
		names := make([]string, 0, len(list.Containers))
		for _, c := range list.Containers {
			names = append(names, c.Name+":"+c.State)
		}
		if want := []string{"tty:running", "finished:exited", "web:running"}; !slices.Equal(names, want) {
			t.Errorf("containers = %v, want %v", names, want)
		}
		web := list.Containers[2]
		wantPorts := []protocol.ContainerPort{{IP: "127.0.0.1", PrivatePort: 80, PublicPort: 8080, Protocol: "tcp"}}
		if !slices.Equal(web.Ports, wantPorts) || web.Stack != "blog" || web.Service != "web" || web.Image != "alpine:3.20" {
			t.Errorf("web = %+v", web)
		}
		// From the captured statistics: 660,788 ns of CPU over 4 s of the
		// machine's 4 CPUs, and 628 KiB of memory with no page cache.
		if math.Abs(web.CPUPercent-0.0660788) > 1e-9 || web.MemoryBytes != 643072 || web.MemoryLimitBytes != 16867737600 {
			t.Errorf("web usage = %v%%, %d of %d", web.CPUPercent, web.MemoryBytes, web.MemoryLimitBytes)
		}
		if finished := list.Containers[1]; finished.CPUPercent != 0 || finished.MemoryBytes != 0 || len(finished.Ports) != 0 {
			t.Errorf("a stopped container has usage: %+v", finished)
		}
	})

	actions := []struct {
		action  docker.ContainerAction
		request protocol.ContainerPayload
		call    string
	}{
		{docker.Start, protocol.ContainerPayload{ID: "web"}, "POST /containers/web/start"},
		{docker.Stop, protocol.ContainerPayload{ID: "web"}, "POST /containers/web/stop"},
		{docker.Restart, protocol.ContainerPayload{ID: "web"}, "POST /containers/web/restart"},
		{docker.Remove, protocol.ContainerPayload{ID: webID, Force: true}, "DELETE /containers/" + webID + "?force=1"},
	}
	for _, a := range actions {
		t.Run(string(a.action)+" sends "+a.call, func(t *testing.T) {
			fake := newFakeEngine(t)
			if err := fake.engine().DoContainer(t.Context(), a.action, a.request); err != nil {
				t.Fatal(err)
			}
			if calls := fake.recorded(); len(calls) != 1 || calls[0] != a.call {
				t.Errorf("calls = %v", calls)
			}
		})
	}

	failures := []struct {
		name   string
		id     string
		status int
		want   error
	}{
		{"a container that doesn't exist", "gone", http.StatusNotFound, docker.ErrNotFound},
		{"removing a running container", "web", http.StatusConflict, docker.ErrConflict},
	}
	for _, f := range failures {
		t.Run("reports "+f.name, func(t *testing.T) {
			fake := newFakeEngine(t)
			fake.fail("DELETE /containers/"+f.id, f.status, "refused")
			err := fake.engine().DoContainer(t.Context(), docker.Remove, protocol.ContainerPayload{ID: f.id})
			if !errors.Is(err, f.want) {
				t.Errorf("err = %v, want %v", err, f.want)
			}
		})
	}

	t.Run("refuses names that could be read as something else, without asking Docker", func(t *testing.T) {
		fake := newFakeEngine(t)
		for _, id := range []string{"", "-f", "../images/x", "web/start", strings.Repeat("a", 256)} {
			if err := fake.engine().DoContainer(t.Context(), docker.Stop, protocol.ContainerPayload{ID: id}); !errors.Is(err, docker.ErrInvalid) {
				t.Errorf("%q: err = %v", id, err)
			}
		}
		if err := fake.engine().DoContainer(t.Context(), "pause", protocol.ContainerPayload{ID: "web"}); !errors.Is(err, docker.ErrInvalid) {
			t.Errorf("unknown action: err = %v", err)
		}
		if calls := fake.recorded(); len(calls) != 0 {
			t.Errorf("calls = %v", calls)
		}
	})

	t.Run("returns Docker's own description of a container", func(t *testing.T) {
		inspected, err := newFakeEngine(t).engine().Inspect(t.Context(), "web")
		if err != nil || !bytes.Contains(inspected.Inspect, []byte(`"Name":"/web"`)) {
			t.Errorf("inspect = %.80s (%v)", inspected.Inspect, err)
		}
	})
}

func TestLogs(t *testing.T) {
	t.Run("keeps stdout and stderr lines in the order they were written", func(t *testing.T) {
		got, err := newFakeEngine(t).engine().Logs(t.Context(), protocol.ContainerLogsPayload{ID: "web", Tail: 4})
		if err != nil {
			t.Fatal(err)
		}
		want := []protocol.ContainerLogLine{
			{Time: milli("2026-11-08T15:52:56.529842769Z"), Stream: "stdout", Text: "GET / 200"},
			{Time: milli("2026-11-08T15:52:56.529864701Z"), Stream: "stderr", Text: "warn: slow"},
			{Time: milli("2026-11-08T15:52:57.530555170Z"), Stream: "stderr", Text: "warn: slow"},
			{Time: milli("2026-11-08T15:52:57.530594021Z"), Stream: "stdout", Text: "GET / 200"},
		}
		if !slices.Equal(got.Lines, want) {
			t.Errorf("lines = %+v", got.Lines)
		}
	})

	t.Run("reads a terminal's log as one raw stream", func(t *testing.T) {
		got, err := newFakeEngine(t).engine().Logs(t.Context(), protocol.ContainerLogsPayload{ID: ttyID})
		want := []protocol.ContainerLogLine{{Time: milli("2026-11-08T15:52:53.823948074Z"), Stream: "stdout", Text: "hello from a terminal"}}
		if err != nil || !slices.Equal(got.Lines, want) {
			t.Errorf("lines = %+v (%v)", got.Lines, err)
		}
	})

	t.Run("follows until the log ends", func(t *testing.T) {
		var lines []string
		err := newFakeEngine(t).engine().Follow(t.Context(), "web", 1794139200000, func(line protocol.ContainerLogLine) {
			lines = append(lines, line.Text)
		})
		if err != nil || len(lines) != 4 {
			t.Errorf("lines = %q (%v)", lines, err)
		}
		if err := newFakeEngine(t).engine().Follow(t.Context(), "-x", 0, nil); !errors.Is(err, docker.ErrInvalid) {
			t.Errorf("-x: %v", err)
		}
	})

	t.Run("refuses to exec in a container that can't be named", func(t *testing.T) {
		if _, err := newFakeEngine(t).engine().ExecStarter("../x", nil); !errors.Is(err, docker.ErrInvalid) {
			t.Errorf("err = %v", err)
		}
		fake := newFakeEngine(t)
		fake.fail("POST /containers/gone/exec", http.StatusNotFound, "No such container: gone")
		start, err := fake.engine().ExecStarter("gone", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := start(80, 24); !errors.Is(err, docker.ErrNotFound) {
			t.Errorf("start = %v", err)
		}
	})

	t.Run("asks for the tail and start requested", func(t *testing.T) {
		fake := newFakeEngine(t)
		if _, err := fake.engine().Logs(t.Context(), protocol.ContainerLogsPayload{ID: "web", Tail: 50, Since: 1794139200500}); err != nil {
			t.Fatal(err)
		}
		if _, err := fake.engine().Logs(t.Context(), protocol.ContainerLogsPayload{ID: "web", Tail: 5001}); !errors.Is(err, docker.ErrInvalid) {
			t.Errorf("tail 5001: %v", err)
		}
	})
}

// frame builds one frame of Docker's multiplexed log format.
func frame(stream byte, text string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(text))) //nolint:gosec // G115: test frames are short
	return append(header, text...)
}

func TestDemultiplex(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
		want  []string
	}{
		{"a line split across frames", slices.Concat(frame(1, "hel"), frame(1, "lo\nnext")), []string{"stdout:hello", "stdout:next"}},
		{"lines without timestamps", frame(2, "plain\n"), []string{"stderr:plain"}},
		{"a frame cut off at the end", slices.Concat(frame(1, "kept\n"), frame(1, "lost")[:10]), []string{"stdout:kept"}},
		{"a header cut off at the end", slices.Concat(frame(1, "kept\n"), []byte{1, 0, 0}), []string{"stdout:kept"}},
		{"invalid UTF-8", frame(1, "bad \xff byte\n"), []string{"stdout:bad � byte"}},
		{"nothing", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			err := docker.Demultiplex(bytes.NewReader(c.input), func(line protocol.ContainerLogLine) {
				got = append(got, line.Stream+":"+line.Text)
			})
			if err != nil || !slices.Equal(got, c.want) {
				t.Errorf("got %q (%v), want %q", got, err, c.want)
			}
		})
	}

	t.Run("refuses a frame far larger than any log line", func(t *testing.T) {
		header := []byte{1, 0, 0, 0, 0x7f, 0xff, 0xff, 0xff}
		if err := docker.Demultiplex(bytes.NewReader(header), func(protocol.ContainerLogLine) {}); err == nil {
			t.Error("no error")
		}
	})

	t.Run("cuts a line with no end", func(t *testing.T) {
		var lengths []int
		err := docker.Demultiplex(bytes.NewReader(frame(1, strings.Repeat("x", 40_000))), func(line protocol.ContainerLogLine) {
			lengths = append(lengths, len(line.Text))
		})
		if err != nil || len(lengths) == 0 || slices.Max(lengths) > 16<<10 {
			t.Errorf("lengths = %v (%v)", lengths, err)
		}
	})
}

func TestImages(t *testing.T) {
	t.Run("lists images with the containers using them", func(t *testing.T) {
		list, err := newFakeEngine(t).engine().Images(t.Context())
		if err != nil || len(list.Images) != 1 {
			t.Fatalf("images = %+v (%v)", list, err)
		}
		alpine := list.Images[0]
		if !slices.Equal(alpine.Tags, []string{"alpine:3.20"}) || alpine.Containers != 3 || alpine.SizeBytes != 12182311 {
			t.Errorf("alpine = %+v", alpine)
		}
	})

	t.Run("pulls with the registry login in the request only", func(t *testing.T) {
		fake := newFakeEngine(t)
		pulled, err := fake.engine().Pull(t.Context(), protocol.ImageRefPayload{Reference: "alpine:3.20",
			Auth: &protocol.RegistryAuth{ServerAddress: "docker.io", Username: "me", Password: "secret"}})
		if err != nil || pulled.Digest != "sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc" {
			t.Errorf("pulled = %+v (%v)", pulled, err)
		}
		if calls := fake.recorded(); len(calls) != 1 || !strings.HasPrefix(calls[0], "POST /images/create?fromImage=docker.io%2Flibrary%2Falpine&tag=3.20") {
			t.Errorf("calls = %v", calls)
		}
	})

	t.Run("reports a pull that fails part way", func(t *testing.T) {
		fake := newFakeEngine(t)
		fake.pullFixture = "pull-stream-error.jsonl"
		if _, err := fake.engine().Pull(t.Context(), protocol.ImageRefPayload{Reference: "alpine"}); err == nil || !strings.Contains(err.Error(), "no space left") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("tells whether the registry has a newer image", func(t *testing.T) {
		check, err := newFakeEngine(t).engine().CheckImage(t.Context(), protocol.ImageRefPayload{Reference: "alpine:3.20"})
		if err != nil || check.UpdateAvailable || check.RegistryDigest != check.LocalDigests[0] {
			t.Errorf("check = %+v (%v)", check, err)
		}
	})

	t.Run("removes by ID or name, and refuses what can't be an image", func(t *testing.T) {
		fake := newFakeEngine(t)
		engine := fake.engine()
		for _, ref := range []string{"sha256:" + strings.Repeat("a", 64), "alpine:3.20"} {
			if err := engine.RemoveImage(t.Context(), protocol.ImageRefPayload{Reference: ref}); err != nil {
				t.Errorf("%s: %v", ref, err)
			}
		}
		for _, ref := range []string{"", "-f", "Upper/Case", "a b"} {
			if err := engine.RemoveImage(t.Context(), protocol.ImageRefPayload{Reference: ref}); !errors.Is(err, docker.ErrInvalid) {
				t.Errorf("%q: %v", ref, err)
			}
			if _, err := engine.Pull(t.Context(), protocol.ImageRefPayload{Reference: ref}); !errors.Is(err, docker.ErrInvalid) {
				t.Errorf("pull %q: %v", ref, err)
			}
		}
		if calls := fake.recorded(); len(calls) != 2 {
			t.Errorf("calls = %v", calls)
		}
	})
}

func TestVolumesAndNetworks(t *testing.T) {
	t.Run("marks the volumes and counts the networks containers use", func(t *testing.T) {
		engine := newFakeEngine(t).engine()
		volumes, err := engine.Volumes(t.Context())
		if err != nil || len(volumes.Volumes) != 1 || !volumes.Volumes[0].InUse || volumes.Volumes[0].Created != milli("2026-11-08T15:52:53Z") {
			t.Errorf("volumes = %+v (%v)", volumes, err)
		}
		networks, err := engine.Networks(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		attached := map[string]int{}
		for _, network := range networks.Networks {
			attached[network.Name] = network.Containers
			if network.BuiltIn != slices.Contains([]string{"bridge", "host", "none"}, network.Name) {
				t.Errorf("%s built in = %v", network.Name, network.BuiltIn)
			}
		}
		if attached["sentinel-net"] != 1 || attached["bridge"] != 2 {
			t.Errorf("attached = %v", attached)
		}
	})

	t.Run("removes unused ones, but never the built-in networks", func(t *testing.T) {
		fake := newFakeEngine(t)
		engine := fake.engine()
		if err := engine.RemoveVolume(t.Context(), "old-data"); err != nil {
			t.Error(err)
		}
		if err := engine.RemoveNetwork(t.Context(), "old-net"); err != nil {
			t.Error(err)
		}
		if err := engine.RemoveNetwork(t.Context(), "bridge"); !errors.Is(err, docker.ErrConflict) {
			t.Errorf("bridge: %v", err)
		}
		fake.fail("DELETE /volumes/in-use", http.StatusConflict, "volume is in use")
		if err := engine.RemoveVolume(t.Context(), "in-use"); !errors.Is(err, docker.ErrConflict) {
			t.Errorf("in use: %v", err)
		}
		if err := engine.RemoveVolume(t.Context(), "../x"); !errors.Is(err, docker.ErrInvalid) {
			t.Errorf("../x: %v", err)
		}
		want := []string{"DELETE /volumes/old-data", "DELETE /networks/old-net", "DELETE /volumes/in-use"}
		if calls := fake.recorded(); !slices.Equal(calls, want) {
			t.Errorf("calls = %v", calls)
		}
	})
}

func TestUnavailable(t *testing.T) {
	t.Run("reports no Docker when no socket is there", func(t *testing.T) {
		engine := docker.New("unix:///nonexistent/docker.sock", "linux")
		if _, err := engine.Containers(t.Context()); !errors.Is(err, docker.ErrUnavailable) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("looks in the usual places for each system", func(t *testing.T) {
		for _, goos := range []string{"linux", "darwin", "windows", "freebsd"} {
			engine := docker.New("", goos)
			if _, err := engine.Describe(t.Context()); err != nil && !errors.Is(err, docker.ErrUnavailable) {
				t.Errorf("%s: %v", goos, err)
			}
		}
	})
}

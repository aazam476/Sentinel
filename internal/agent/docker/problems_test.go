// SPDX-License-Identifier: Apache-2.0

package docker_test

import (
	"reflect"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/docker"
	"github.com/aazam476/sentinel/internal/protocol"
)

func TestProblemsAreUnexpectedStopsAndRestartLoops(t *testing.T) {
	containers := []protocol.Container{
		{Name: "web", Image: "nginx", State: "running", Status: "Up 3 hours (healthy)"},
		{Name: "worker", Image: "app", State: "exited", Status: "Exited (137) 3 minutes ago"},
		{Name: "backup", Image: "restic", State: "exited", Status: "Exited (0) 2 hours ago"},
		{Name: "db", Image: "postgres", State: "restarting", Status: "Restarting (1) 5 seconds ago"},
		{Name: "crashed", Image: "app", State: "dead", Status: "Exited (1) 1 day ago"},
		{Name: "new", Image: "app", State: "created", Status: "Created"},
		{Name: "odd", Image: "app", State: "exited", Status: "Exited"},
	}
	want := []protocol.ContainerProblem{
		{Name: "crashed", Image: "app", State: "exited", ExitCode: 1},
		{Name: "db", Image: "postgres", State: "restarting"},
		{Name: "worker", Image: "app", State: "exited", ExitCode: 137},
	}
	if got := docker.FindProblems(containers); !reflect.DeepEqual(got, want) {
		t.Errorf("Problems = %+v\nwant %+v", got, want)
	}
	if got := docker.FindProblems(nil); got == nil || len(got) != 0 {
		t.Errorf("FindProblems(nil) = %#v, want an empty list", got)
	}
}

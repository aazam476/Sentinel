// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/aazam476/sentinel/internal/protocol"
)

// exitStatus reads the exit code from Docker's and Podman's status text,
// "Exited (137) 3 minutes ago".
var exitStatus = regexp.MustCompile(`^Exited \((-?\d+)\)`)

// FindProblems picks, from a list, the containers that stopped unexpectedly (exited with a
// non-zero code) or are restarting in a loop (§9.9), sorted by name so the
// same problems always read the same. A container stopped cleanly, with
// code 0, is not a problem.
func FindProblems(containers []protocol.Container) []protocol.ContainerProblem {
	problems := []protocol.ContainerProblem{}
	for _, container := range containers {
		problem := protocol.ContainerProblem{Name: container.Name, Image: container.Image, State: container.State}
		switch container.State {
		case "restarting":
			problems = append(problems, problem)
		case "exited", "dead":
			code, known := exitCode(container.Status)
			if known && code != 0 {
				problem.State, problem.ExitCode = "exited", code
				problems = append(problems, problem)
			}
		}
	}
	slices.SortFunc(problems, func(a, b protocol.ContainerProblem) int { return strings.Compare(a.Name, b.Name) })
	return problems
}

// exitCode is the code in a status, and whether there was one.
func exitCode(status string) (int, bool) {
	match := exitStatus.FindStringSubmatch(strings.TrimSpace(status))
	if match == nil {
		return 0, false
	}
	code, err := strconv.Atoi(match[1])
	return code, err == nil
}

// Problems lists the containers on this engine that stopped unexpectedly
// or keep restarting.
func (e *Engine) Problems(ctx context.Context) ([]protocol.ContainerProblem, error) {
	list, err := e.Containers(ctx)
	if err != nil {
		return nil, err
	}
	return FindProblems(list.Containers), nil
}

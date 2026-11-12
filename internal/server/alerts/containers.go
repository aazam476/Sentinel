// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aazam476/sentinel/internal/protocol"
)

// The kinds of container alert (§9.9).
const (
	KindContainerStopped    = "container.stopped"
	KindContainerRestarting = "container.restarting"
)

// ContainerKinds lists every container alert kind: the ones a
// docker.health event raises or resolves.
func ContainerKinds() []string {
	return []string{KindContainerStopped, KindContainerRestarting}
}

// ContainerConditions are the alerts a machine's container problems call
// for, one per container.
func ContainerConditions(health protocol.DockerHealthPayload) []Condition {
	conditions := make([]Condition, 0, len(health.Problems))
	for _, problem := range health.Problems {
		condition := Condition{Subject: problem.Name, Severity: Warning}
		if problem.State == "restarting" {
			condition.Kind = KindContainerRestarting
			condition.Message = fmt.Sprintf("Container %s (%s) keeps restarting.", problem.Name, problem.Image)
		} else {
			condition.Kind = KindContainerStopped
			condition.Message = fmt.Sprintf("Container %s (%s) stopped unexpectedly with exit code %d.", problem.Name, problem.Image, problem.ExitCode)
		}
		conditions = append(conditions, condition)
	}
	return conditions
}

// RecordDockerHealth raises and resolves a machine's container alerts from
// its docker.health event, by the first enabled container rule that
// watches it, and returns whether any changed.
func (s *Service) RecordDockerHealth(ctx context.Context, agentID int64, health protocol.DockerHealthPayload) (bool, error) {
	var rules []Rule
	var target machine
	var found bool
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		if rules, err = readRules(ctx, tx, RuleContainer); err != nil {
			return err
		}
		target, found, err = readMachine(ctx, tx, agentID)
		return err
	})
	if err != nil || !found {
		return false, err
	}
	var conditions []Condition
	for _, rule := range rules {
		if rule.appliesTo(target) {
			for _, condition := range ContainerConditions(health) {
				condition.RuleID = rule.ID
				conditions = append(conditions, condition)
			}
			break
		}
	}
	return s.reconcile(ctx, scope{agentID: agentID, kinds: ContainerKinds()}, conditions)
}

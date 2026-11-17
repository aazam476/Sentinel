// SPDX-License-Identifier: Apache-2.0

package alerts_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
)

func countAlerts(t *testing.T, h *harness, filter string) int {
	t.Helper()
	var count int
	err := h.database.Read(t.Context(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM alerts WHERE `+filter).Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func problems(count int, prefix string) protocol.DockerHealthPayload {
	health := protocol.DockerHealthPayload{}
	for index := range count {
		health.Problems = append(health.Problems, protocol.ContainerProblem{Name: fmt.Sprintf("%s-%d", prefix, index), Image: "app", State: "exited", ExitCode: 1})
	}
	return health
}

func TestAMachineCantGrowTheAlertsWithoutBound(t *testing.T) {
	t.Run("keeps at most 100 alerts from one report, and trims long text", func(t *testing.T) {
		h := newHarness(t)
		health := problems(500, "c")
		health.Problems[0].Name = strings.Repeat("n", 5000)
		health.Problems[0].Image = strings.Repeat("i", 50000)
		if _, err := h.service.RecordDockerHealth(t.Context(), h.agentID, health); err != nil {
			t.Fatal(err)
		}
		if got := countAlerts(t, h, `state = 'firing'`); got != 100 {
			t.Errorf("%d firing alerts, want 100", got)
		}
		if got := countAlerts(t, h, `length(subject) > 200 OR length(message) > 4000`); got != 0 {
			t.Errorf("%d alerts with untrimmed text", got)
		}
	})

	t.Run("prunes resolved alerts past 90 days and beyond the newest 500 a machine", func(t *testing.T) {
		h := newHarness(t)
		for round := range 8 {
			// Each report invents new names, resolving the last ones.
			if _, err := h.service.RecordDockerHealth(t.Context(), h.agentID, problems(100, fmt.Sprint("r", round))); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.service.Prune(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := countAlerts(t, h, `state = 'resolved'`); got != 500 {
			t.Errorf("%d resolved alerts kept, want 500", got)
		}
		if got := countAlerts(t, h, `state = 'firing'`); got != 100 {
			t.Errorf("%d firing alerts, want the 100 still firing", got)
		}
		h.clock.Advance(91 * 24 * time.Hour)
		if err := h.service.Prune(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := countAlerts(t, h, `state = 'resolved'`); got != 0 {
			t.Errorf("%d resolved alerts past 90 days, want 0", got)
		}
		if got := countAlerts(t, h, `state = 'firing'`); got != 100 {
			t.Errorf("pruning touched %d firing alerts", 100-got)
		}
	})
}

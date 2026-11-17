// SPDX-License-Identifier: Apache-2.0

package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Bounds on what machines can put into the alerts table: their reports
// name containers, pools and disks, and a machine could otherwise invent
// new names until the database fills.
const (
	maxSubjectLength      = 200
	maxMessageLength      = 4000
	maxConditions         = 100
	keepResolvedFor       = 90 * 24 * time.Hour
	maxResolvedPerMachine = 500
)

// bounded keeps at most maxConditions conditions, with their text trimmed.
func bounded(conditions []Condition) []Condition {
	conditions = conditions[:min(len(conditions), maxConditions)]
	for index := range conditions {
		conditions[index].Subject = trim(conditions[index].Subject, maxSubjectLength)
		conditions[index].Message = trim(conditions[index].Message, maxMessageLength)
	}
	return conditions
}

// trim cuts text to at most limit bytes, on a character boundary, ending
// in "…" when cut.
func trim(text string, limit int) string {
	text = strings.ToValidUTF8(text, "�")
	if len(text) <= limit {
		return text
	}
	cut := limit - len("…")
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// Prune deletes resolved alerts older than 90 days, and keeps at most the
// newest 500 resolved alerts per machine. Firing alerts are never pruned.
func (s *Service) Prune(ctx context.Context) error {
	before := s.clock.Now().Add(-keepResolvedFor).UnixMilli()
	err := s.database.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM alerts WHERE state = 'resolved' AND resolved_at < ?`, before); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM alerts WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY agent_id ORDER BY resolved_at DESC, id DESC) AS newest
				FROM alerts WHERE state = 'resolved'
			) WHERE newest > ?)`, maxResolvedPerMachine)
		return err
	})
	if err != nil {
		return fmt.Errorf("prune resolved alerts: %w", err)
	}
	return nil
}

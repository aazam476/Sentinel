// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Batch is one script run on some machines.
type Batch struct {
	ID         int64
	ScriptID   *int64
	ScriptName string
	Language   string
	Body       string
	// StartedBy is the person who started it; ScheduleID the schedule.
	StartedBy  string
	ScheduleID *int64
	Target     string
	StartedAt  int64
	// Counts says how many runs are in each state.
	Counts map[string]int
	Runs   []Run
}

// Run is one machine's run in a batch. Output is only filled in by
// RunOutput.
type Run struct {
	ID          int64
	BatchID     int64
	AgentID     int64
	AgentPublic string
	AgentName   string
	State       string
	ExitCode    *int
	Error       string
	StartedAt   int64
	FinishedAt  *int64
	Output      string
	Truncated   bool
}

// Batches lists the newest batches, with how many runs are in each state.
func (s *Service) Batches(ctx context.Context, limit int) ([]Batch, error) {
	var batches []Batch
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT b.id, b.script_id, b.script_name, b.language, b.body, COALESCE(u.username, ''),
			b.schedule_id, b.target, b.started_at FROM script_batches b LEFT JOIN users u ON u.id = b.started_by
			ORDER BY b.started_at DESC, b.id DESC LIMIT ?`, limit)
		if err != nil {
			return fmt.Errorf("list batches: %w", err)
		}
		defer func() { _ = rows.Close() }()
		batches = []Batch{}
		for rows.Next() {
			var b Batch
			if err := rows.Scan(&b.ID, &b.ScriptID, &b.ScriptName, &b.Language, &b.Body, &b.StartedBy, &b.ScheduleID, &b.Target, &b.StartedAt); err != nil {
				return fmt.Errorf("read a batch: %w", err)
			}
			batches = append(batches, b)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("list batches: %w", err)
		}
		return s.countRuns(ctx, tx, batches)
	})
	return batches, err
}

func (s *Service) countRuns(ctx context.Context, tx *sql.Tx, batches []Batch) error {
	for i := range batches {
		batches[i].Counts = map[string]int{}
		rows, err := tx.QueryContext(ctx, `SELECT state, COUNT(*) FROM script_runs WHERE batch_id = ? GROUP BY state`, batches[i].ID)
		if err != nil {
			return fmt.Errorf("count runs: %w", err)
		}
		for rows.Next() {
			var state string
			var count int
			if err := rows.Scan(&state, &count); err != nil {
				_ = rows.Close()
				return fmt.Errorf("count runs: %w", err)
			}
			batches[i].Counts[state] = count
		}
		_ = rows.Close()
	}
	return nil
}

const runColumns = `r.id, r.batch_id, r.agent_id, a.public_id, CASE WHEN a.display_name = '' THEN a.hostname ELSE a.display_name END,
	r.state, r.exit_code, r.error, r.started_at, r.finished_at`

func scanRun(row scanner, extra ...any) (Run, error) {
	var r Run
	err := row.Scan(append([]any{&r.ID, &r.BatchID, &r.AgentID, &r.AgentPublic, &r.AgentName, &r.State, &r.ExitCode, &r.Error,
		&r.StartedAt, &r.FinishedAt}, extra...)...)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("read a run: %w", err)
	}
	return r, nil
}

// GetBatch returns a batch with its runs, by machine name.
func (s *Service) GetBatch(ctx context.Context, id int64) (Batch, error) {
	batches, err := s.batchByID(ctx, id)
	if err != nil {
		return Batch{}, err
	}
	batch := batches[0]
	err = s.database.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+runColumns+` FROM script_runs r JOIN agents a ON a.id = r.agent_id
			WHERE r.batch_id = ? ORDER BY 5 COLLATE NOCASE, r.id`, id)
		if err != nil {
			return fmt.Errorf("list runs: %w", err)
		}
		defer func() { _ = rows.Close() }()
		batch.Runs = []Run{}
		for rows.Next() {
			run, err := scanRun(rows)
			if err != nil {
				return err
			}
			batch.Runs = append(batch.Runs, run)
		}
		return rows.Err()
	})
	return batch, err
}

func (s *Service) batchByID(ctx context.Context, id int64) ([]Batch, error) {
	var batch Batch
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT b.id, b.script_id, b.script_name, b.language, b.body, COALESCE(u.username, ''),
			b.schedule_id, b.target, b.started_at FROM script_batches b LEFT JOIN users u ON u.id = b.started_by WHERE b.id = ?`, id).
			Scan(&batch.ID, &batch.ScriptID, &batch.ScriptName, &batch.Language, &batch.Body, &batch.StartedBy, &batch.ScheduleID, &batch.Target, &batch.StartedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read the batch: %w", err)
		}
		list := []Batch{batch}
		if err := s.countRuns(ctx, tx, list); err != nil {
			return err
		}
		batch = list[0]
		return nil
	})
	return []Batch{batch}, err
}

// RunOutput returns one run with its output: as far as it has got while
// it runs, or as stored once it ended.
func (s *Service) RunOutput(ctx context.Context, id int64) (Run, error) {
	var run Run
	var stored string
	var truncated bool
	err := s.database.Read(ctx, func(tx *sql.Tx) error {
		var err error
		run, err = scanRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+`, r.output, r.output_truncated FROM script_runs r
			JOIN agents a ON a.id = r.agent_id WHERE r.id = ?`, id), &stored, &truncated)
		return err
	})
	if err != nil {
		return Run{}, err
	}
	run.Output, run.Truncated = stored, truncated
	s.mutex.Lock()
	live, running := s.live[id]
	s.mutex.Unlock()
	if running {
		run.Output, run.Truncated = live.snapshot()
	}
	return run, nil
}

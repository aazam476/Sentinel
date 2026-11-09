// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// maxOutput is how much of a run's output is kept.
const maxOutput = 1 << 20

// Run states.
const (
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
	StateTimedOut  = "timed_out"
	StateOffline   = "offline"
	StateSkipped   = "skipped"
	StateCanceled  = "canceled"
)

// Target says which machines a script runs on.
type Target struct {
	// Kind is "agent" (Value is a machine's ID), "tag" (a tag), or "agents"
	// (machine IDs, comma-separated).
	Kind  string
	Value string
}

// String describes the target for people, as batches show it.
func (t Target) String() string {
	switch t.Kind {
	case "tag":
		return "tag " + t.Value
	case "agents":
		return fmt.Sprintf("%d machines", len(strings.Split(t.Value, ",")))
	}
	return "one machine"
}

// resolve finds the target's machines, every one that isn't revoked.
func (s *Service) resolve(ctx context.Context, target Target) ([]fleet.Agent, error) {
	agents, err := s.fleet.AllAgents(ctx)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for id := range strings.SplitSeq(target.Value, ",") {
		wanted[strings.TrimSpace(id)] = true
	}
	var chosen []fleet.Agent
	for _, agent := range agents {
		switch target.Kind {
		case "tag":
			if containsFold(agent.Tags, target.Value) {
				chosen = append(chosen, agent)
			}
		case "agent", "agents":
			if wanted[agent.PublicID] {
				chosen = append(chosen, agent)
			}
		default:
			return nil, &InvalidError{"target", "choose a machine, a tag or several machines"}
		}
	}
	if len(chosen) == 0 {
		return nil, &InvalidError{"target", "no machine matches it"}
	}
	return chosen, nil
}

func containsFold(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

// Start runs a script on the target's machines, for a person, and returns
// the batch's ID at once; the runs carry on in the background.
func (s *Service) Start(ctx context.Context, actor auth.Session, request auth.RequestInfo, scriptID int64, target Target) (int64, error) {
	if err := auth.Authorize(actor, auth.PermissionRunScripts); err != nil {
		return 0, err
	}
	userID := actor.UserID
	entry := audit.Entry{UserID: &userID, Username: actor.Username, Action: "script.run", Outcome: audit.OutcomeSuccess, IP: request.IP}
	return s.start(ctx, scriptID, target, starter{userID: &userID, entry: entry})
}

// starter is who or what started a batch.
type starter struct {
	userID     *int64
	scheduleID *int64
	entry      audit.Entry
}

func (s *Service) start(ctx context.Context, scriptID int64, target Target, by starter) (int64, error) {
	script, err := s.GetScript(ctx, scriptID)
	if err != nil {
		return 0, err
	}
	agents, err := s.resolve(ctx, target)
	if err != nil {
		return 0, err
	}
	now := s.clock.Now().UnixMilli()
	var batchID int64
	runs := make([]int64, len(agents))
	err = s.database.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO script_batches (script_id, script_name, language, body, started_by, schedule_id, target, started_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, script.ID, script.Name, script.Language, script.Body, by.userID, by.scheduleID, target.String(), now)
		if err != nil {
			return fmt.Errorf("start the batch: %w", err)
		}
		if batchID, err = result.LastInsertId(); err != nil {
			return fmt.Errorf("start the batch: %w", err)
		}
		for i, agent := range agents {
			if runs[i], err = s.insertRun(ctx, tx, batchID, agent, script, now); err != nil {
				return err
			}
		}
		by.entry.Details = map[string]any{"script": script.Name, "batch": batchID, "target": target.String(), "machines": len(agents)}
		if by.scheduleID != nil {
			by.entry.Details["schedule"] = *by.scheduleID
		}
		return s.audit.RecordInTx(ctx, tx, by.entry)
	})
	if err != nil {
		return 0, err
	}
	for i, agent := range agents {
		if s.stateOf(runs[i]) == StateRunning {
			s.running.Go(func() { s.execute(context.WithoutCancel(ctx), runs[i], agent.ID, script) })
		}
	}
	return batchID, nil
}

// insertRun adds a run, already finished if the machine is of the wrong
// system or offline.
func (s *Service) insertRun(ctx context.Context, tx *sql.Tx, batchID int64, agent fleet.Agent, script Script, now int64) (int64, error) {
	state, problem := StateRunning, ""
	switch {
	case script.TargetOS != "any" && !strings.EqualFold(agent.OS, script.TargetOS):
		state, problem = StateSkipped, "The script is for "+script.TargetOS+"; this machine runs "+agent.OS+"."
	case !s.hub.IsOnline(agent.ID):
		state, problem = StateOffline, "The machine was offline."
	}
	var finished any
	if state != StateRunning {
		finished = now
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO script_runs (batch_id, agent_id, state, error, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?)`, batchID, agent.ID, state, problem, now, finished)
	if err != nil {
		return 0, fmt.Errorf("add a run: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("add a run: %w", err)
	}
	if state == StateRunning {
		s.mutex.Lock()
		s.live[id] = &liveRun{}
		s.mutex.Unlock()
	}
	return id, nil
}

func (s *Service) stateOf(runID int64) string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, running := s.live[runID]; running {
		return StateRunning
	}
	return ""
}

// liveRun is a running script's output so far, and a way to stop it.
type liveRun struct {
	mutex     sync.Mutex
	output    strings.Builder
	truncated bool
	cancel    context.CancelFunc
}

func (l *liveRun) add(data []byte) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	room := maxOutput - l.output.Len()
	if len(data) > room {
		data, l.truncated = data[:max(room, 0)], true
	}
	l.output.Write(data)
}

func (l *liveRun) snapshot() (string, bool) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.output.String(), l.truncated
}

// execute runs the script on one machine and stores how it ended.
func (s *Service) execute(ctx context.Context, runID, agentID int64, script Script) {
	s.mutex.Lock()
	live := s.live[runID]
	ctx, live.cancel = context.WithCancel(ctx)
	s.mutex.Unlock()
	defer live.cancel()
	result := s.stream(ctx, agentID, script, live)
	output, truncated := live.snapshot()
	if err := s.finish(context.WithoutCancel(ctx), runID, result, output, truncated); err != nil {
		s.logger.Error("could not store a script run", "run", runID, "error", err)
	}
	s.mutex.Lock()
	delete(s.live, runID)
	s.mutex.Unlock()
}

// stream opens the script's stream and collects its output until it ends.
func (s *Service) stream(ctx context.Context, agentID int64, script Script, live *liveRun) protocol.ScriptResult {
	stream, err := s.hub.OpenScript(ctx, agentID, func(id protocol.StreamID) any {
		return protocol.ScriptRunPayload{StreamID: id, Language: script.Language, Body: script.Body, TimeoutSeconds: script.TimeoutSeconds}
	})
	if err != nil {
		var refusal *protocol.Error
		if errors.As(err, &refusal) {
			return protocol.ScriptResult{ExitCode: -1, Error: refusal.Message}
		}
		return protocol.ScriptResult{ExitCode: -1, Error: "The script couldn't start: " + err.Error()}
	}
	for {
		select {
		case frame := <-stream.Frames():
			live.add(frame)
		case <-stream.Done():
			for drained := false; !drained; {
				select {
				case frame := <-stream.Frames():
					live.add(frame)
				default:
					drained = true
				}
			}
			return protocol.ParseScriptResult(stream.Reason())
		case <-ctx.Done():
			stream.Close(context.WithoutCancel(ctx), "canceled")
			return protocol.ScriptResult{ExitCode: -1, Error: "Canceled before it finished."}
		}
	}
}

// finish stores a run's result and output.
func (s *Service) finish(ctx context.Context, runID int64, result protocol.ScriptResult, output string, truncated bool) error {
	state := StateSucceeded
	switch {
	case result.TimedOut:
		state = StateTimedOut
	case result.Error == "Canceled before it finished.":
		state = StateCanceled
	case result.ExitCode != 0 || result.Error != "":
		state = StateFailed
	}
	var exitCode any
	if result.ExitCode >= 0 {
		exitCode = result.ExitCode
	}
	return s.database.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE script_runs SET state = ?, exit_code = ?, error = ?, output = ?, output_truncated = ?, finished_at = ?
			WHERE id = ?`, state, exitCode, result.Error, output, truncated, s.clock.Now().UnixMilli(), runID)
		if err != nil {
			return fmt.Errorf("finish the run: %w", err)
		}
		return nil
	})
}

// Cancel stops a running script.
func (s *Service) Cancel(actor auth.Session, runID int64) error {
	if err := auth.Authorize(actor, auth.PermissionRunScripts); err != nil {
		return err
	}
	s.mutex.Lock()
	var cancel context.CancelFunc
	if live, running := s.live[runID]; running {
		cancel = live.cancel
	}
	s.mutex.Unlock()
	if cancel == nil {
		return ErrNotFound
	}
	cancel()
	return nil
}

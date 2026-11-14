// SPDX-License-Identifier: Apache-2.0

package scripts_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
	"github.com/aazam476/sentinel/internal/server/scripts"
	"github.com/aazam476/sentinel/internal/server/store"
	"github.com/aazam476/sentinel/internal/testutil/fakeclock"
)

// fakeStream plays back output, then ends with reason; with hold it waits
// to be closed.
type fakeStream struct {
	frames chan []byte
	done   chan struct{}
	once   sync.Once
	reason string
	closed chan string
}

func (f *fakeStream) Frames() <-chan []byte { return f.frames }
func (f *fakeStream) Done() <-chan struct{} { return f.done }
func (f *fakeStream) Reason() string        { return f.reason }
func (f *fakeStream) Close(_ context.Context, reason string) {
	f.once.Do(func() { f.closed <- reason; close(f.done) })
}

// plan is how a fake machine answers a script.
type plan struct {
	output  []string
	result  protocol.ScriptResult
	refusal *protocol.Error
	hold    bool
	// reason, if set, ends the stream with it instead of the result, as
	// a dropped connection does.
	reason string
}

type fakeHub struct {
	mutex   sync.Mutex
	online  map[int64]bool
	plans   map[int64]plan
	asked   []protocol.ScriptRunPayload
	streams map[int64]*fakeStream
}

func (h *fakeHub) IsOnline(agentID int64) bool { return h.online[agentID] }

func (h *fakeHub) OpenScript(_ context.Context, agentID int64, payload func(protocol.StreamID) any) (scripts.Stream, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	request, _ := payload(1).(protocol.ScriptRunPayload)
	h.asked = append(h.asked, request)
	p := h.plans[agentID]
	if p.refusal != nil {
		return nil, p.refusal
	}
	stream := &fakeStream{frames: make(chan []byte, len(p.output)+1), done: make(chan struct{}), closed: make(chan string, 1)}
	for _, chunk := range p.output {
		stream.frames <- []byte(chunk)
	}
	if !p.hold {
		stream.reason = p.result.Reason()
		if p.reason != "" {
			stream.reason = p.reason
		}
		close(stream.done)
	}
	h.streams[agentID] = stream
	return stream, nil
}

// fakeAlerts records how scheduled runs ended, as "agent schedule script: failure".
type fakeAlerts struct {
	mutex    sync.Mutex
	recorded []string
}

func (f *fakeAlerts) RecordScheduledRun(_ context.Context, agentID, scheduleID int64, scriptName, failure string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.recorded = append(f.recorded, fmt.Sprintf("%d %d %s: %s", agentID, scheduleID, scriptName, failure))
	return nil
}

func (f *fakeAlerts) take() []string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	recorded := f.recorded
	f.recorded = nil
	return recorded
}

type fakeFleet struct{ agents []fleet.Agent }

func (f fakeFleet) AllAgents(context.Context) ([]fleet.Agent, error) { return f.agents, nil }

// fakeStepUp answers every second-factor check with err: nil for someone
// who checked recently, auth.ErrStepUpRequired for someone who didn't.
type fakeStepUp struct{ err error }

func (f *fakeStepUp) RequireRecentSecondFactor(auth.Session) error { return f.err }

type harness struct {
	service  *scripts.Service
	hub      *fakeHub
	clock    *fakeclock.Clock
	auditLog *audit.Log
	operator auth.Session
	viewer   auth.Session
	agents   []fleet.Agent
	alerts   *fakeAlerts
	// stepUp says whether the person checked their second factor recently;
	// they did, unless a test sets its err.
	stepUp *fakeStepUp
	// options are what the service was built with, for a test that builds
	// another.
	options scripts.Options
}

// newHarness has three machines: nas (linux, tagged homelab, online), pc
// (windows, tagged homelab, online) and laptop (linux, offline).
func newHarness(t *testing.T) *harness {
	t.Helper()
	clock := fakeclock.New(time.Date(2026, 11, 8, 2, 59, 0, 0, time.UTC))
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "sentinel.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() }) // nothing to check when a test database closes
	h := &harness{clock: clock, auditLog: audit.New(database), alerts: &fakeAlerts{}, stepUp: &fakeStepUp{}}
	err = database.Write(t.Context(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(t.Context(), `INSERT INTO users (username, password_hash, role, created_at) VALUES ('olga', 'x', 'operator', 0)`)
		if err != nil {
			return err
		}
		userID, _ := result.LastInsertId()
		verified := int64(1)
		h.operator = auth.Session{UserID: userID, Username: "olga", Role: auth.RoleOperator, SecondFactorAt: &verified}
		h.viewer = auth.Session{UserID: userID, Username: "olga", Role: auth.RoleViewer, SecondFactorAt: &verified}
		for i, host := range []string{"nas", "pc", "laptop"} {
			id := "01JBX3K5Q7Z8M4N6P8R0T2V4W" + string(rune('0'+i))
			result, err := tx.ExecContext(t.Context(), `INSERT INTO agents (public_id, public_key, hostname, os, arch, agent_version, protocol_version, enrolled_at)
				VALUES (?, x'00', ?, 'linux', 'amd64', 'v1', 1, 0)`, id, host)
			if err != nil {
				return err
			}
			agentID, _ := result.LastInsertId()
			agent := fleet.Agent{ID: agentID, PublicID: id, Hostname: host, OS: "linux"}
			if host != "laptop" {
				agent.Tags = []string{"HomeLab"}
			}
			if host == "pc" {
				agent.OS = "windows"
			}
			h.agents = append(h.agents, agent)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.hub = &fakeHub{online: map[int64]bool{h.agents[0].ID: true, h.agents[1].ID: true}, plans: map[int64]plan{}, streams: map[int64]*fakeStream{}}
	h.options = scripts.Options{Database: database, AuditLog: h.auditLog, Hub: h.hub, Fleet: fakeFleet{h.agents},
		Clock: clock, Logger: slog.New(slog.DiscardHandler), Alerts: h.alerts, StepUp: h.stepUp}
	h.service = scripts.New(h.options)
	return h
}

func (h *harness) save(t *testing.T, script scripts.Script) scripts.Script {
	t.Helper()
	saved, err := h.service.SaveScript(t.Context(), h.operator, auth.RequestInfo{IP: "203.0.113.7"}, script)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func uptime() scripts.Script {
	return scripts.Script{Name: "Uptime", Language: protocol.LanguageSh, Body: "uptime", TargetOS: "linux"}
}

func (h *harness) actions(t *testing.T) []string {
	t.Helper()
	records, err := h.auditLog.List(t.Context(), audit.ListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for i := len(records) - 1; i >= 0; i-- {
		actions = append(actions, records[i].Action)
	}
	return actions
}

func TestLibrary(t *testing.T) {
	t.Run("saves, lists, changes and deletes scripts, with defaults and an audit entry each", func(t *testing.T) {
		h := newHarness(t)
		saved := h.save(t, scripts.Script{Name: "  zfs scrub status ", Language: protocol.LanguageBash, Body: "zpool status"})
		if saved.Name != "zfs scrub status" || saved.TargetOS != "any" || saved.TimeoutSeconds != 600 || saved.ID == 0 {
			t.Errorf("saved %+v", saved)
		}
		h.save(t, uptime())
		saved.Body = "zpool status -x"
		h.save(t, saved)
		list, err := h.service.Scripts(t.Context())
		if err != nil || len(list) != 2 || list[0].Name != "Uptime" || list[1].Body != "zpool status -x" {
			t.Fatalf("list %+v, %v", list, err)
		}
		if err := h.service.DeleteScript(t.Context(), h.operator, auth.RequestInfo{}, saved.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.GetScript(t.Context(), saved.ID); !errors.Is(err, scripts.ErrNotFound) {
			t.Errorf("deleted script: %v", err)
		}
		if got := strings.Join(h.actions(t), " "); got != "script.save script.save script.save script.delete" {
			t.Errorf("audit = %s", got)
		}
	})

	invalid := []struct {
		name   string
		script scripts.Script
		field  string
	}{
		{"a script without a name", scripts.Script{Language: "sh", Body: "x"}, "name"},
		{"an unknown language", scripts.Script{Name: "x", Language: "perl", Body: "x"}, "language"},
		{"an unknown system", scripts.Script{Name: "x", Language: "sh", Body: "x", TargetOS: "plan9"}, "targetOS"},
		{"an empty script", scripts.Script{Name: "x", Language: "sh", Body: "  \n"}, "body"},
		{"a script over 256 KB", scripts.Script{Name: "x", Language: "sh", Body: strings.Repeat("x", 300<<10)}, "body"},
		{"a time limit over a day", scripts.Script{Name: "x", Language: "sh", Body: "x", TimeoutSeconds: 90000}, "timeoutSeconds"},
		{"a description too long", scripts.Script{Name: "x", Language: "sh", Body: "x", Description: strings.Repeat("d", 1001)}, "description"},
		{"bytes that aren't text", scripts.Script{Name: "x", Language: "sh", Body: "\xff\xfe"}, "body"},
	}
	for _, tc := range invalid {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, err := h.service.SaveScript(t.Context(), h.operator, auth.RequestInfo{}, tc.script)
			var invalidErr *scripts.InvalidError
			if !errors.As(err, &invalidErr) || invalidErr.Field != tc.field {
				t.Errorf("got %v, want a problem with %s", err, tc.field)
			}
		})
	}

	t.Run("refuses a second script with the same name, a missing one, and viewers", func(t *testing.T) {
		h := newHarness(t)
		h.save(t, uptime())
		if _, err := h.service.SaveScript(t.Context(), h.operator, auth.RequestInfo{}, uptime()); !errors.Is(err, scripts.ErrExists) {
			t.Errorf("same name: %v", err)
		}
		missing := uptime()
		missing.ID, missing.Name = 999, "Other"
		if _, err := h.service.SaveScript(t.Context(), h.operator, auth.RequestInfo{}, missing); !errors.Is(err, scripts.ErrNotFound) {
			t.Errorf("missing: %v", err)
		}
		if _, err := h.service.SaveScript(t.Context(), h.viewer, auth.RequestInfo{}, uptime()); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("viewer: %v", err)
		}
		if err := h.service.DeleteScript(t.Context(), h.operator, auth.RequestInfo{}, 999); !errors.Is(err, scripts.ErrNotFound) {
			t.Errorf("delete missing: %v", err)
		}
	})
}

func TestRuns(t *testing.T) {
	t.Run("runs on a tag: skips the wrong system, marks the offline, and keeps each machine's output", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		nas := h.agents[0]
		h.hub.plans[nas.ID] = plan{output: []string{" 12:00 up 3 days,", " load 0.10\n"}, result: protocol.ScriptResult{ExitCode: 0}}
		batchID, err := h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, scripts.Target{Kind: "tag", Value: "homelab"})
		if err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		batch, err := h.service.GetBatch(t.Context(), batchID)
		if err != nil {
			t.Fatal(err)
		}
		if batch.Target != "tag homelab" || batch.StartedBy != "olga" || len(batch.Runs) != 2 {
			t.Fatalf("batch %+v", batch)
		}
		states := map[string]string{}
		for _, run := range batch.Runs {
			states[run.AgentName] = run.State
		}
		if states["nas"] != scripts.StateSucceeded || states["pc"] != scripts.StateSkipped {
			t.Errorf("states %v", states)
		}
		run, err := h.service.RunOutput(t.Context(), batch.Runs[0].ID)
		if err != nil || run.Output != " 12:00 up 3 days, load 0.10\n" || run.ExitCode == nil || *run.ExitCode != 0 {
			t.Errorf("run %+v, %v", run, err)
		}
		if len(h.hub.asked) != 1 || h.hub.asked[0].Body != "uptime" || h.hub.asked[0].TimeoutSeconds != 600 {
			t.Errorf("asked %+v", h.hub.asked)
		}
	})

	outcomes := []struct {
		name  string
		plan  plan
		state string
		err   string
	}{
		{"a failure", plan{result: protocol.ScriptResult{ExitCode: 2}}, scripts.StateFailed, ""},
		{"a time limit", plan{result: protocol.ScriptResult{ExitCode: -1, TimedOut: true, Error: "stopped"}}, scripts.StateTimedOut, "stopped"},
		{"a refusal", plan{refusal: &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "Running scripts is disabled by this machine's local policy."}},
			scripts.StateFailed, "Running scripts is disabled by this machine's local policy."},
		{"a dropped connection", plan{reason: "the agent disconnected"}, scripts.StateFailed, "the agent disconnected"},
	}
	for _, tc := range outcomes {
		t.Run("records "+tc.name, func(t *testing.T) {
			h := newHarness(t)
			script := h.save(t, uptime())
			nas := h.agents[0]
			h.hub.plans[nas.ID] = tc.plan
			batchID, err := h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, scripts.Target{Kind: "agent", Value: nas.PublicID})
			if err != nil {
				t.Fatal(err)
			}
			h.service.Wait()
			batch, _ := h.service.GetBatch(t.Context(), batchID)
			if run := batch.Runs[0]; run.State != tc.state || (tc.err != "" && run.Error != tc.err) || run.FinishedAt == nil {
				t.Errorf("run %+v", run)
			}
		})
	}

	t.Run("keeps the first 1 MB of output and says it cut the rest", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		nas := h.agents[0]
		h.hub.plans[nas.ID] = plan{output: []string{strings.Repeat("a", 900_000), strings.Repeat("b", 300_000)}}
		batchID, _ := h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, scripts.Target{Kind: "agent", Value: nas.PublicID})
		h.service.Wait()
		batch, _ := h.service.GetBatch(t.Context(), batchID)
		run, _ := h.service.RunOutput(t.Context(), batch.Runs[0].ID)
		if len(run.Output) != 1<<20 || !run.Truncated {
			t.Errorf("kept %d bytes, truncated %v", len(run.Output), run.Truncated)
		}
	})

	t.Run("shows a run's output while it runs, and stops it when canceled", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		nas := h.agents[0]
		h.hub.plans[nas.ID] = plan{output: []string{"working...\n"}, hold: true}
		batchID, _ := h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, scripts.Target{Kind: "agents", Value: nas.PublicID})
		batch, _ := h.service.GetBatch(t.Context(), batchID)
		runID := batch.Runs[0].ID
		for {
			run, _ := h.service.RunOutput(t.Context(), runID)
			if run.Output == "working...\n" {
				break
			}
		}
		if err := h.service.Cancel(h.operator, runID); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		run, _ := h.service.RunOutput(t.Context(), runID)
		if run.State != scripts.StateCanceled || run.Output != "working...\n" {
			t.Errorf("run %+v", run)
		}
		if err := h.service.Cancel(h.operator, runID); !errors.Is(err, scripts.ErrNotFound) {
			t.Errorf("cancel again: %v", err)
		}
	})

	t.Run("refuses a target that matches no machine, or isn't a target", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		for _, target := range []scripts.Target{{Kind: "tag", Value: "nothing"}, {Kind: "everything", Value: "x"}} {
			_, err := h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, target)
			var invalidErr *scripts.InvalidError
			if !errors.As(err, &invalidErr) || invalidErr.Field != "target" {
				t.Errorf("%+v: %v", target, err)
			}
		}
		if _, err := h.service.Start(t.Context(), h.viewer, auth.RequestInfo{}, script.ID, scripts.Target{Kind: "tag", Value: "homelab"}); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("viewer: %v", err)
		}
	})

	t.Run("lists batches newest first, with how many runs ended how", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		for range 2 {
			if _, err := h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, scripts.Target{Kind: "agents", Value: h.agents[0].PublicID + "," + h.agents[2].PublicID}); err != nil {
				t.Fatal(err)
			}
			h.clock.Advance(time.Minute)
		}
		h.service.Wait()
		batches, err := h.service.Batches(t.Context(), 10)
		if err != nil || len(batches) != 2 || batches[0].ID < batches[1].ID {
			t.Fatalf("batches %+v, %v", batches, err)
		}
		if counts := batches[0].Counts; counts[scripts.StateSucceeded] != 1 || counts[scripts.StateOffline] != 1 {
			t.Errorf("counts %v", counts)
		}
		if batches[0].Target != "2 machines" {
			t.Errorf("target %q", batches[0].Target)
		}
	})
}

func TestSchedules(t *testing.T) {
	t.Run("runs a schedule on a tag when its time comes, then sets the next time", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		h.hub.plans[h.agents[0].ID] = plan{output: []string{"ok\n"}}
		schedule, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, scripts.Schedule{
			ScriptID: script.ID, Cron: "0 3 * * *", Timezone: "UTC", Target: scripts.Target{Kind: "tag", Value: "homelab"}, Enabled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		threeAM := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
		if schedule.NextRunAt == nil || *schedule.NextRunAt != threeAM.UnixMilli() {
			t.Fatalf("next %v", schedule.NextRunAt)
		}
		if err := h.service.RunDue(t.Context()); err != nil {
			t.Fatal(err)
		}
		if batches, _ := h.service.Batches(t.Context(), 10); len(batches) != 0 {
			t.Fatalf("ran early: %+v", batches)
		}
		h.clock.Set(threeAM)
		if err := h.service.RunDue(t.Context()); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		batches, _ := h.service.Batches(t.Context(), 10)
		if len(batches) != 1 || batches[0].ScheduleID == nil || *batches[0].ScheduleID != schedule.ID || batches[0].StartedBy != "" {
			t.Fatalf("batches %+v", batches)
		}
		list, _ := h.service.Schedules(t.Context())
		if *list[0].NextRunAt != threeAM.Add(24*time.Hour).UnixMilli() || *list[0].LastRunAt != threeAM.UnixMilli() {
			t.Errorf("schedule %+v", list[0])
		}
		if got := h.actions(t); got[len(got)-1] != "script.run" {
			t.Errorf("audit %v", got)
		}
	})

	invalid := []struct {
		name     string
		schedule scripts.Schedule
		field    string
	}{
		{"a cron that isn't one", scripts.Schedule{Cron: "every day", Target: scripts.Target{Kind: "tag", Value: "x"}, Enabled: true}, "cron"},
		{"an unknown time zone", scripts.Schedule{Cron: "@daily", Timezone: "Mars/Olympus", Target: scripts.Target{Kind: "tag", Value: "x"}, Enabled: true}, "timezone"},
		{"no target", scripts.Schedule{Cron: "@daily", Enabled: true}, "target"},
		{"a day that never comes", scripts.Schedule{Cron: "0 0 30 2 *", Target: scripts.Target{Kind: "tag", Value: "x"}, Enabled: true}, "cron"},
		{"a script not in the library", scripts.Schedule{ScriptID: 999, Cron: "@daily", Target: scripts.Target{Kind: "tag", Value: "x"}, Enabled: true}, "scriptId"},
	}
	for _, tc := range invalid {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			h := newHarness(t)
			script := h.save(t, uptime())
			if tc.schedule.ScriptID == 0 {
				tc.schedule.ScriptID = script.ID
			}
			_, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, tc.schedule)
			var invalidErr *scripts.InvalidError
			if !errors.As(err, &invalidErr) || invalidErr.Field != tc.field {
				t.Errorf("got %v, want a problem with %s", err, tc.field)
			}
		})
	}

	t.Run("a disabled schedule has no next time and doesn't run; deleting one keeps its runs", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		schedule, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, scripts.Schedule{
			ScriptID: script.ID, Cron: "* * * * *", Target: scripts.Target{Kind: "tag", Value: "homelab"},
		})
		if err != nil || schedule.NextRunAt != nil {
			t.Fatalf("got %+v, %v", schedule, err)
		}
		h.clock.Advance(time.Hour)
		if err := h.service.RunDue(t.Context()); err != nil {
			t.Fatal(err)
		}
		if batches, _ := h.service.Batches(t.Context(), 10); len(batches) != 0 {
			t.Errorf("a disabled schedule ran")
		}
		if err := h.service.DeleteSchedule(t.Context(), h.operator, auth.RequestInfo{}, schedule.ID); err != nil {
			t.Fatal(err)
		}
		if err := h.service.DeleteSchedule(t.Context(), h.operator, auth.RequestInfo{}, schedule.ID); !errors.Is(err, scripts.ErrNotFound) {
			t.Errorf("again: %v", err)
		}
	})
}

func TestScheduledRunAlerts(t *testing.T) {
	cases := []struct {
		name string
		plan plan
		want string
	}{
		{"reports a success, so an earlier failure's alert resolves", plan{output: []string{"ok\n"}}, ""},
		{"reports a non-zero exit code", plan{result: protocol.ScriptResult{ExitCode: 2}}, "it exited with code 2."},
		{"reports a timeout", plan{result: protocol.ScriptResult{ExitCode: -1, TimedOut: true}}, "it timed out."},
		{"reports why the machine refused it", plan{refusal: &protocol.Error{Code: protocol.ErrorPolicyDenied, Message: "Scripts are turned off."}}, "Scripts are turned off."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			script := h.save(t, uptime())
			h.hub.plans[h.agents[0].ID] = tc.plan
			schedule, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, scripts.Schedule{
				ScriptID: script.ID, Cron: "* * * * *", Target: scripts.Target{Kind: "tag", Value: "homelab"}, Enabled: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			h.clock.Advance(time.Minute)
			if err := h.service.RunDue(t.Context()); err != nil {
				t.Fatal(err)
			}
			h.service.Wait()
			// The pc is skipped (the script is for Linux), so only the nas reports.
			want := []string{fmt.Sprintf("%d %d Uptime: %s", h.agents[0].ID, schedule.ID, tc.want)}
			if got := h.alerts.take(); !slices.Equal(got, want) {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}

	t.Run("doesn't report runs a person started", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		h.hub.plans[h.agents[0].ID] = plan{result: protocol.ScriptResult{ExitCode: 1}}
		if _, err := h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, scripts.Target{Kind: "agent", Value: h.agents[0].PublicID}); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		if got := h.alerts.take(); len(got) != 0 {
			t.Errorf("got %q", got)
		}
	})
}

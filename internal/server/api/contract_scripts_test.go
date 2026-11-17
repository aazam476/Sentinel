// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
	"github.com/aazam476/sentinel/internal/server/hub"
	"github.com/aazam476/sentinel/internal/server/scripts"
)

// serveScript answers the next script.run as the agent does: at once,
// then the output on the stream, then the result as the close reason.
func serveScript(t *testing.T, conn *websocket.Conn, output string, result protocol.ScriptResult) <-chan protocol.ScriptRunPayload {
	t.Helper()
	ran := make(chan protocol.ScriptRunPayload, 1)
	go func() {
		for {
			message, frame, err := protocol.ReadAny(t.Context(), conn)
			if err != nil {
				return
			}
			if frame != nil || message.Type != protocol.TypeRequest || message.Action != protocol.ActionScriptRun {
				continue
			}
			var request protocol.ScriptRunPayload
			_ = json.Unmarshal(message.Payload, &request)
			if protocol.WriteMessage(t.Context(), conn, protocol.Message{Type: protocol.TypeResponse, ID: message.ID}, struct{}{}) != nil {
				return
			}
			_ = protocol.WriteFrame(t.Context(), conn, request.StreamID, []byte(output))
			closing := protocol.Message{Type: protocol.TypeStreamClose}
			_ = protocol.WriteMessage(t.Context(), conn, closing, protocol.StreamClosePayload{StreamID: request.StreamID, Reason: result.Reason()})
			ran <- request
			return
		}
	}()
	return ran
}

func TestScripts(t *testing.T) {
	t.Run("keeps viewers away from scripts", func(t *testing.T) {
		f := newTerminalFixture(t)
		viewer := f.server.addUser(f.admin, "viewer", "viewer")
		expect(t, viewer.do(http.MethodGet, "/api/scripts", nil), http.StatusForbidden, "forbidden")
		expect(t, viewer.do(http.MethodGet, "/api/script-batches", nil), http.StatusForbidden, "forbidden")
	})

	t.Run("saves a script, runs it on a machine, and shows its output and exit code", func(t *testing.T) {
		f := newTerminalFixture(t)
		var script api.Script
		save := api.SaveScriptRequest{Name: "Disk usage", Language: "sh", Body: "df -h /", TargetOS: "any", TimeoutSeconds: 60}
		f.admin.browser.do(http.MethodPost, "/api/scripts", save).decode(t, &script)
		if script.ID == 0 || script.Name != "Disk usage" {
			t.Fatalf("script = %+v", script)
		}
		ran := serveScript(t, f.agent, "/dev/sda1  46G  12G  32G  27% /\n", protocol.ScriptResult{ExitCode: 0})
		var started api.RunScriptResponse
		run := api.RunScriptRequest{Target: api.ScriptTarget{Kind: "agent", Value: f.agentID}}
		f.admin.browser.do(http.MethodPost, "/api/scripts/"+strconv.FormatInt(script.ID, 10)+"/run", run).decode(t, &started)
		if request := <-ran; request.Body != "df -h /" || request.TimeoutSeconds != 60 || request.Language != "sh" {
			t.Errorf("agent ran %+v", request)
		}
		f.server.app.Scripts.Wait()

		var batch api.ScriptBatchDetails
		f.admin.browser.do(http.MethodGet, "/api/script-batches/"+strconv.FormatInt(started.BatchID, 10), nil).decode(t, &batch)
		if len(batch.Runs) != 1 || batch.Runs[0].State != "succeeded" || batch.Runs[0].AgentID != f.agentID || batch.Body != "df -h /" {
			t.Fatalf("batch = %+v", batch)
		}
		var output api.ScriptRunOutput
		f.admin.browser.do(http.MethodGet, "/api/script-runs/"+strconv.FormatInt(batch.Runs[0].ID, 10), nil).decode(t, &output)
		if output.Output != "/dev/sda1  46G  12G  32G  27% /\n" || output.ExitCode == nil || *output.ExitCode != 0 {
			t.Errorf("output = %+v", output)
		}
		var batches api.ScriptBatches
		f.admin.browser.do(http.MethodGet, "/api/script-batches", nil).decode(t, &batches)
		if len(batches.Batches) != 1 || batches.Batches[0].Counts["succeeded"] != 1 || batches.Batches[0].StartedBy != "admin" {
			t.Errorf("batches = %+v", batches)
		}
		if got := auditActions(t, f.server, "script."); !slices.Equal(got, []string{"script.save:success", "script.run:success"}) {
			t.Errorf("audit = %v", got)
		}
	})

	t.Run("saves a script as long as a script may be, and explains why a longer one isn't", func(t *testing.T) {
		f := newTerminalFixture(t)
		line := `echo "backing up" && restic backup /srv` + "\n"
		save := api.SaveScriptRequest{Name: "Long", Language: "sh", Body: strings.Repeat(line, scripts.MaxBodyBytes/len(line))}
		got := f.admin.browser.do(http.MethodPost, "/api/scripts", save)
		expect(t, got, http.StatusOK, "")
		var script api.Script
		got.decode(t, &script)
		if script.Body != save.Body {
			t.Errorf("kept %d bytes of the script, want %d", len(script.Body), len(save.Body))
		}
		save.Name, save.Body = "Longer", save.Body+line
		got = f.admin.browser.do(http.MethodPost, "/api/scripts", save)
		expect(t, got, http.StatusBadRequest, "invalid_input")
		if !strings.Contains(string(got.body), "256 KB") {
			t.Errorf("the refusal doesn't name the limit: %s", got.body)
		}
	})

	t.Run("explains a script it can't save, and a run it can't start", func(t *testing.T) {
		f := newTerminalFixture(t)
		got := f.admin.browser.do(http.MethodPost, "/api/scripts", api.SaveScriptRequest{Name: "x", Language: "cobol", Body: "x"})
		expect(t, got, http.StatusBadRequest, "invalid_input")
		var script api.Script
		f.admin.browser.do(http.MethodPost, "/api/scripts", api.SaveScriptRequest{Name: "x", Language: "sh", Body: "x"}).decode(t, &script)
		expect(t, f.admin.browser.do(http.MethodPost, "/api/scripts", api.SaveScriptRequest{Name: "x", Language: "sh", Body: "y"}), http.StatusConflict, "conflict")
		noMatch := api.RunScriptRequest{Target: api.ScriptTarget{Kind: "tag", Value: "nothing"}}
		expect(t, f.admin.browser.do(http.MethodPost, "/api/scripts/"+strconv.FormatInt(script.ID, 10)+"/run", noMatch), http.StatusBadRequest, "invalid_input")
		expect(t, f.admin.browser.do(http.MethodPost, "/api/scripts/999/run", noMatch), http.StatusNotFound, "not_found")
		expect(t, f.admin.browser.do(http.MethodGet, "/api/script-batches/abc", nil), http.StatusNotFound, "not_found")
		expect(t, f.admin.browser.do(http.MethodPost, "/api/script-runs/999/cancel", nil), http.StatusNotFound, "not_found")
		expect(t, f.admin.browser.do(http.MethodDelete, "/api/scripts/"+strconv.FormatInt(script.ID, 10), nil), http.StatusNoContent, "")
		expect(t, f.admin.browser.do(http.MethodDelete, "/api/scripts/"+strconv.FormatInt(script.ID, 10), nil), http.StatusNotFound, "not_found")
	})

	t.Run("adds, lists, changes and deletes schedules", func(t *testing.T) {
		f := newTerminalFixture(t)
		var script api.Script
		f.admin.browser.do(http.MethodPost, "/api/scripts", api.SaveScriptRequest{Name: "Backup", Language: "sh", Body: "restic backup /srv"}).decode(t, &script)
		bad := api.SaveScheduleRequest{ScriptID: script.ID, Cron: "every night", Timezone: "UTC", Target: api.ScriptTarget{Kind: "tag", Value: "nas"}, Enabled: true}
		got := f.admin.browser.do(http.MethodPost, "/api/schedules", bad)
		expect(t, got, http.StatusBadRequest, "invalid_input")
		var schedule api.Schedule
		good := bad
		good.Cron = "0 3 * * 1-5"
		f.admin.browser.do(http.MethodPost, "/api/schedules", good).decode(t, &schedule)
		if schedule.ID == 0 || schedule.NextRunAt == nil || schedule.ScriptName != "" && schedule.ScriptName != "Backup" {
			t.Fatalf("schedule = %+v", schedule)
		}
		good.Enabled = false
		f.admin.browser.do(http.MethodPut, "/api/schedules/"+strconv.FormatInt(schedule.ID, 10), good).decode(t, &schedule)
		var list api.Schedules
		f.admin.browser.do(http.MethodGet, "/api/schedules", nil).decode(t, &list)
		if len(list.Schedules) != 1 || list.Schedules[0].Enabled || list.Schedules[0].NextRunAt != nil || list.Schedules[0].ScriptName != "Backup" {
			t.Errorf("list = %+v", list)
		}
		expect(t, f.admin.browser.do(http.MethodDelete, "/api/schedules/"+strconv.FormatInt(schedule.ID, 10), nil), http.StatusNoContent, "")
		if got := auditActions(t, f.server, "schedule."); !slices.Equal(got, []string{"schedule.save:success", "schedule.save:success", "schedule.delete:success"}) {
			t.Errorf("audit = %v", got)
		}
	})
}

// TestSEC07_RunningAScriptNeedsARecentSecondFactorOverHTTP checks the
// ways a person can make a script run: running it, on one machine or many,
// switching a schedule on, and changing a script that is scheduled. Each
// needs a second factor checked in the last 15 minutes (ADR 0056).
func TestSEC07_RunningAScriptNeedsARecentSecondFactorOverHTTP(t *testing.T) {
	server := newTestServer(t)
	admin := server.setUpAdmin()
	token := createToken(t, admin, api.CreateEnrollmentTokenRequest{MaxUses: 2})
	var script api.Script
	save := api.SaveScriptRequest{Name: "Uptime", Language: "sh", Body: "uptime"}
	admin.browser.do(http.MethodPost, "/api/scripts", save).decode(t, &script)
	// The admin's check goes stale before the machines connect, so their
	// connections don't sit silent while the clock moves.
	server.clock.Advance(16 * time.Minute)
	events, stop := server.app.Hub.Subscribe()
	defer stop()
	machines := map[string]*websocket.Conn{}
	tags := []string{"fleet"}
	for range 2 {
		id, conn := server.connectAgent(token.Secret, token.ServerFingerprint)
		waitForAgentEvent(t, events, id, hub.EventInfoUpdated)
		expect(t, admin.browser.do(http.MethodPatch, "/api/agents/"+id, api.UpdateAgentRequest{Tags: &tags}), http.StatusOK, "")
		machines[id] = conn
	}
	var one string
	for id := range machines {
		one = id
	}
	scriptPath := "/api/scripts/" + strconv.FormatInt(script.ID, 10)
	onTag := api.RunScriptRequest{Target: api.ScriptTarget{Kind: "tag", Value: "fleet"}}
	schedule := api.SaveScheduleRequest{ScriptID: script.ID, Cron: "0 3 * * *", Timezone: "UTC", Target: onTag.Target, Enabled: true}

	// Without a recent check: running anywhere, even on one machine, and
	// switching a schedule on are refused.
	onOne := api.RunScriptRequest{Target: api.ScriptTarget{Kind: "agent", Value: one}}
	expect(t, admin.browser.do(http.MethodPost, scriptPath+"/run", onTag), http.StatusForbidden, "step_up_required")
	expect(t, admin.browser.do(http.MethodPost, scriptPath+"/run", onOne), http.StatusForbidden, "step_up_required")
	expect(t, admin.browser.do(http.MethodPost, "/api/schedules", schedule), http.StatusForbidden, "step_up_required")
	if got := auditActions(t, server, "script.run"); len(got) != 0 {
		t.Fatalf("a refused run was recorded as run: %v", got)
	}

	// After one: the schedule saves, and the script runs on both.
	expect(t, admin.browser.do(http.MethodPost, "/api/auth/step-up", api.TOTPCodeRequest{TOTPCode: server.code(admin.totpSecret)}), http.StatusOK, "")
	ran := serveScript(t, machines[one], "up 3 days\n", protocol.ScriptResult{})
	expect(t, admin.browser.do(http.MethodPost, scriptPath+"/run", onOne), http.StatusOK, "")
	<-ran
	server.app.Scripts.Wait()
	expect(t, admin.browser.do(http.MethodPost, "/api/schedules", schedule), http.StatusOK, "")
	var everyone []<-chan protocol.ScriptRunPayload
	for _, conn := range machines {
		everyone = append(everyone, serveScript(t, conn, "up 3 days\n", protocol.ScriptResult{}))
	}
	var started api.RunScriptResponse
	admin.browser.do(http.MethodPost, scriptPath+"/run", onTag).decode(t, &started)
	for _, machine := range everyone {
		<-machine
	}
	server.app.Scripts.Wait()
	var batch api.ScriptBatchDetails
	admin.browser.do(http.MethodGet, "/api/script-batches/"+strconv.FormatInt(started.BatchID, 10), nil).decode(t, &batch)
	if len(batch.Runs) != 2 {
		t.Errorf("ran on %d machines, want 2", len(batch.Runs))
	}

	// The script is now scheduled on the tag: changing it needs the check.
	server.clock.Advance(16 * time.Minute)
	save.Body = "uptime; id"
	expect(t, admin.browser.do(http.MethodPut, scriptPath, save), http.StatusForbidden, "step_up_required")
	expect(t, admin.browser.do(http.MethodPost, "/api/auth/step-up", api.TOTPCodeRequest{TOTPCode: server.code(admin.totpSecret)}), http.StatusOK, "")
	expect(t, admin.browser.do(http.MethodPut, scriptPath, save), http.StatusOK, "")
}

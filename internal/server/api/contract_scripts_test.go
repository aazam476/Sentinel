// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
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

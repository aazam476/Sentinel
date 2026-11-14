// SPDX-License-Identifier: Apache-2.0

package scripts_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/scripts"
)

// stale makes the harness's person someone whose last second-factor check
// was more than 15 minutes ago; fresh, someone who has just checked.
func (h *harness) stale() { h.stepUp.err = auth.ErrStepUpRequired }
func (h *harness) fresh() { h.stepUp.err = nil }

// together lists machines for an "agents" target.
func together(first, second string) string { return first + "," + second }

func TestSEC07_RunningAScriptOnMoreThanOneMachineNeedsARecentSecondFactor(t *testing.T) {
	cases := []struct {
		name string
		// target is where the script runs, given the nas, the pc and the
		// laptop's IDs.
		target     func(nas, pc string) scripts.Target
		machines   int
		wantStepUp bool
	}{
		{"one machine needs no check", func(nas, _ string) scripts.Target { return scripts.Target{Kind: "agent", Value: nas} }, 1, false},
		{"a tag on two machines needs one", func(_, _ string) scripts.Target { return scripts.Target{Kind: "tag", Value: "homelab"} }, 2, true},
		{"a tag on one machine needs none", func(_, _ string) scripts.Target { return scripts.Target{Kind: "tag", Value: "edge"} }, 1, false},
		{"two machines picked by hand need one", func(nas, pc string) scripts.Target {
			return scripts.Target{Kind: "agents", Value: together(nas, pc)}
		}, 2, true},
		{"two machines passed off as one need one", func(nas, pc string) scripts.Target {
			return scripts.Target{Kind: "agent", Value: together(nas, pc)}
		}, 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.agents[2].Tags = []string{"edge"} // the laptop alone
			script := h.save(t, uptime())
			target := tc.target(h.agents[0].PublicID, h.agents[1].PublicID)

			h.stale()
			batchID, err := h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, target)
			if tc.wantStepUp {
				if !errors.Is(err, auth.ErrStepUpRequired) {
					t.Fatalf("without a recent check: %v, want a step-up", err)
				}
				if batches, _ := h.service.Batches(t.Context(), 10); len(batches) != 0 || len(h.hub.asked) != 0 {
					t.Fatalf("something ran without the check: %+v, %+v", batches, h.hub.asked)
				}
				h.fresh()
				batchID, err = h.service.Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			h.service.Wait()
			if batch, _ := h.service.GetBatch(t.Context(), batchID); len(batch.Runs) != tc.machines {
				t.Errorf("ran on %d machines, want %d", len(batch.Runs), tc.machines)
			}
		})
	}

	t.Run("a server built without the check refuses, rather than skip it", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		options := h.options
		options.StepUp = nil
		_, err := scripts.New(options).Start(t.Context(), h.operator, auth.RequestInfo{}, script.ID, scripts.Target{Kind: "tag", Value: "homelab"})
		if err == nil || len(h.hub.asked) != 0 {
			t.Errorf("ran on a tag without any check: %v, %+v", err, h.hub.asked)
		}
	})
}

func TestSEC07_ScheduleThatReachesMoreThanOneMachineNeedsARecentSecondFactor(t *testing.T) {
	cases := []struct {
		name       string
		target     func(nas, pc string) scripts.Target
		enabled    bool
		wantStepUp bool
	}{
		{"on a tag", func(_, _ string) scripts.Target { return scripts.Target{Kind: "tag", Value: "homelab"} }, true, true},
		{"on a tag only one machine has today", func(_, _ string) scripts.Target { return scripts.Target{Kind: "tag", Value: "edge"} }, true, true},
		{"on two machines", func(nas, pc string) scripts.Target {
			return scripts.Target{Kind: "agents", Value: together(nas, pc)}
		}, true, true},
		{"on two machines passed off as one", func(nas, pc string) scripts.Target {
			return scripts.Target{Kind: "agent", Value: together(nas, pc)}
		}, true, true},
		{"on one machine", func(nas, _ string) scripts.Target { return scripts.Target{Kind: "agent", Value: nas} }, true, false},
		{"on one machine named twice", func(nas, _ string) scripts.Target {
			return scripts.Target{Kind: "agents", Value: together(nas, nas)}
		}, true, false},
		{"on a tag, but paused", func(_, _ string) scripts.Target { return scripts.Target{Kind: "tag", Value: "homelab"} }, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			script := h.save(t, uptime())
			schedule := scripts.Schedule{ScriptID: script.ID, Cron: "0 3 * * *", Enabled: tc.enabled,
				Target: tc.target(h.agents[0].PublicID, h.agents[1].PublicID)}

			h.stale()
			_, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, schedule)
			if tc.wantStepUp {
				if !errors.Is(err, auth.ErrStepUpRequired) {
					t.Fatalf("without a recent check: %v, want a step-up", err)
				}
				if list, _ := h.service.Schedules(t.Context()); len(list) != 0 {
					t.Fatalf("saved without the check: %+v", list)
				}
				h.fresh()
				_, err = h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, schedule)
			}
			if err != nil {
				t.Fatal(err)
			}
			if list, _ := h.service.Schedules(t.Context()); len(list) != 1 {
				t.Errorf("schedules %+v", list)
			}
		})
	}

	t.Run("switching a paused one on needs the check, pausing it doesn't", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		h.stale()
		paused, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, scripts.Schedule{
			ScriptID: script.ID, Cron: "0 3 * * *", Target: scripts.Target{Kind: "tag", Value: "homelab"},
		})
		if err != nil {
			t.Fatal(err)
		}
		on := paused
		on.Enabled = true
		if _, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, on); !errors.Is(err, auth.ErrStepUpRequired) {
			t.Fatalf("switched on without a recent check: %v", err)
		}
		h.fresh()
		if _, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, on); err != nil {
			t.Fatal(err)
		}
		h.stale()
		if _, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, paused); err != nil {
			t.Errorf("pausing it again: %v", err)
		}
	})

	t.Run("once saved, it runs by itself, whoever checked what since", func(t *testing.T) {
		h := newHarness(t)
		script := h.save(t, uptime())
		_, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, scripts.Schedule{
			ScriptID: script.ID, Cron: "0 3 * * *", Target: scripts.Target{Kind: "tag", Value: "homelab"}, Enabled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		h.stale()
		h.clock.Set(time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC))
		if err := h.service.RunDue(t.Context()); err != nil {
			t.Fatal(err)
		}
		h.service.Wait()
		batches, _ := h.service.Batches(t.Context(), 10)
		if len(batches) != 1 {
			t.Fatalf("batches %+v", batches)
		}
		if batch, _ := h.service.GetBatch(t.Context(), batches[0].ID); len(batch.Runs) != 2 {
			t.Errorf("ran on %d machines, want the tag's 2", len(batch.Runs))
		}
	})
}

func TestSEC07_ChangingAScriptScheduledOnMoreThanOneMachineNeedsARecentSecondFactor(t *testing.T) {
	cases := []struct {
		name       string
		schedule   *scripts.Schedule
		wantStepUp bool
	}{
		{"a script no schedule runs", nil, false},
		{"a script scheduled on a tag", &scripts.Schedule{Target: scripts.Target{Kind: "tag", Value: "homelab"}, Enabled: true}, true},
		{"a script scheduled on a tag, but paused", &scripts.Schedule{Target: scripts.Target{Kind: "tag", Value: "homelab"}}, false},
		{"a script scheduled on one machine", &scripts.Schedule{Target: scripts.Target{Kind: "agent"}, Enabled: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			script := h.save(t, uptime())
			if tc.schedule != nil {
				schedule := *tc.schedule
				schedule.ScriptID, schedule.Cron = script.ID, "0 3 * * *"
				if schedule.Target.Kind == "agent" {
					schedule.Target.Value = h.agents[0].PublicID
				}
				if _, err := h.service.SaveSchedule(t.Context(), h.operator, auth.RequestInfo{}, schedule); err != nil {
					t.Fatal(err)
				}
			}
			changed := script
			changed.Body = "uptime; id"

			h.stale()
			_, err := h.service.SaveScript(t.Context(), h.operator, auth.RequestInfo{}, changed)
			if tc.wantStepUp {
				if !errors.Is(err, auth.ErrStepUpRequired) {
					t.Fatalf("without a recent check: %v, want a step-up", err)
				}
				if kept, _ := h.service.GetScript(t.Context(), script.ID); kept.Body != "uptime" {
					t.Fatalf("the script changed without the check: %q", kept.Body)
				}
				h.fresh()
				_, err = h.service.SaveScript(t.Context(), h.operator, auth.RequestInfo{}, changed)
			}
			if err != nil {
				t.Fatal(err)
			}
			if saved, _ := h.service.GetScript(t.Context(), script.ID); saved.Body != "uptime; id" {
				t.Errorf("body %q", saved.Body)
			}
		})
	}
}

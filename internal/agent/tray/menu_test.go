// SPDX-License-Identifier: Apache-2.0

package tray_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

func labels(model tray.Model) []string {
	out := make([]string, 0, len(model.Items))
	for _, item := range model.Items {
		label := item.Label
		if item.Action != nil {
			label = "[" + label + "]"
		}
		out = append(out, label)
	}
	return out
}

func TestMenuShowsWhatTheAgentIsDoing(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		status      tray.Status
		reachable   bool
		wantIcon    tray.Icon
		wantTooltip string
		wantLabels  []string
	}{
		{
			name: "says when the agent isn't running", reachable: false,
			wantIcon: tray.IconOff, wantTooltip: "Sentinel: the agent isn't running",
			wantLabels: []string{"The Sentinel agent isn't running"},
		},
		{
			name: "names the server it is connected to", reachable: true,
			status:   tray.Status{Connected: true, Server: "https://sentinel.example.com:7443", Version: "v1.2.0"},
			wantIcon: tray.IconIdle, wantTooltip: "Sentinel: connected to sentinel.example.com:7443",
			wantLabels: []string{"Connected to sentinel.example.com:7443", "", "[Pause remote access]", "Sentinel agent v1.2.0"},
		},
		{
			name: "says it is retrying while disconnected", reachable: true,
			status:   tray.Status{Server: "not a url"},
			wantIcon: tray.IconOff, wantTooltip: "Sentinel: not connected",
			wantLabels: []string{"Not connected to not a url; retrying", "", "[Pause remote access]"},
		},
		{
			name: "lists open sessions with a way to end each", reachable: true,
			status: tray.Status{Connected: true, ConsentRequired: true, Sessions: []tray.Session{
				{ID: "d1", Kind: tray.KindDesktop, User: "admin", Since: now.Add(-time.Hour)},
				{ID: "t1", Kind: tray.KindTerminal, User: "", Since: now.Add(-25 * time.Hour)},
			}},
			wantIcon: tray.IconActive, wantTooltip: "Sentinel: 2 remote sessions open",
			wantLabels: []string{
				"Connected to the server", "Remote desktop asks you first", "",
				"[End admin's view of this screen (since 14:00)]", "[End someone's terminal (since Nov 10 14:00)]",
				"", "[Pause remote access]",
			},
		},
		{
			name: "offers to resume when paused", reachable: true,
			status:   tray.Status{Connected: true, Paused: true},
			wantIcon: tray.IconPaused, wantTooltip: "Sentinel: remote access paused",
			wantLabels: []string{"Connected to the server", "", "[Resume remote access]"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := tray.Menu(test.status, test.reachable, now)
			if model.Icon != test.wantIcon || model.Tooltip != test.wantTooltip {
				t.Errorf("icon %v %q, want %v %q", model.Icon, model.Tooltip, test.wantIcon, test.wantTooltip)
			}
			if got := labels(model); !reflect.DeepEqual(got, test.wantLabels) {
				t.Errorf("labels = %q\nwant %q", got, test.wantLabels)
			}
		})
	}
}

func TestMenuActionsNameWhatToDo(t *testing.T) {
	model := tray.Menu(tray.Status{Connected: true, Sessions: []tray.Session{{ID: "t1", Kind: tray.KindTerminal}}}, true, time.Now())
	var got []tray.Action
	for _, item := range model.Items {
		if item.Action != nil {
			got = append(got, *item.Action)
		}
	}
	want := []tray.Action{{Kind: tray.ActionEnd, Session: "t1"}, {Kind: tray.ActionPause}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("actions = %+v, want %+v", got, want)
	}
}

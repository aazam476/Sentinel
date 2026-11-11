// SPDX-License-Identifier: Apache-2.0

package winui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

func TestEntriesNumberTheActionsAndKeepLabelsLiteral(t *testing.T) {
	model := tray.Model{Items: []tray.Item{
		{Label: "Connected to R&D server"},
		{},
		{Label: "End admin's terminal", Action: &tray.Action{Kind: tray.ActionEnd, Session: "t1"}},
		{Label: "Pause remote access", Action: &tray.Action{Kind: tray.ActionPause}},
	}}
	lines, actions := entries(model)
	want := []entry{
		{label: "Connected to R&&D server"},
		{separator: true},
		{label: "End admin's terminal", command: 1, enabled: true},
		{label: "Pause remote access", command: 2, enabled: true},
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("entries = %+v\nwant %+v", lines, want)
	}
	if actions[1] != (tray.Action{Kind: tray.ActionEnd, Session: "t1"}) || actions[2].Kind != tray.ActionPause || len(actions) != 2 {
		t.Errorf("actions = %+v", actions)
	}
}

func TestTooltipFitsTheNotificationArea(t *testing.T) {
	if got := tooltip("Sentinel: connected"); got != "Sentinel: connected" {
		t.Errorf("short tooltip = %q", got)
	}
	if got := []rune(tooltip(strings.Repeat("é", 300))); len(got) != 127 || got[126] != '…' {
		t.Errorf("long tooltip has %d characters", len(got))
	}
}

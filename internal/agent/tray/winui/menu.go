// SPDX-License-Identifier: Apache-2.0

package winui

import (
	"strings"

	"github.com/aazam476/sentinel/internal/agent/tray"
)

// firstCommand is the first menu command ID; 0 means no pick.
const firstCommand = 1

// entry is one line of the native menu.
type entry struct {
	label     string
	command   uint16
	enabled   bool
	separator bool
}

// entries turns the model into menu lines, and the actions their command
// IDs stand for.
func entries(model tray.Model) ([]entry, map[uint16]tray.Action) {
	lines := make([]entry, 0, len(model.Items))
	actions := map[uint16]tray.Action{}
	command := uint16(firstCommand)
	for _, item := range model.Items {
		switch {
		case item.Label == "":
			lines = append(lines, entry{separator: true})
		case item.Action == nil:
			lines = append(lines, entry{label: escapeMnemonic(item.Label)})
		default:
			lines = append(lines, entry{label: escapeMnemonic(item.Label), command: command, enabled: true})
			actions[command] = *item.Action
			command++
		}
	}
	return lines, actions
}

// escapeMnemonic keeps an & in a label from underlining the next letter.
func escapeMnemonic(label string) string {
	return strings.ReplaceAll(label, "&", "&&")
}

// tooltip fits the icon's tooltip: 127 characters at most.
func tooltip(text string) string {
	runes := []rune(text)
	if len(runes) > 127 {
		return string(runes[:126]) + "…"
	}
	return text
}

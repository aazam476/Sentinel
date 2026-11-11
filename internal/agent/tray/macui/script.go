// SPDX-License-Identifier: Apache-2.0

package macui

import (
	"strings"
)

// The dialog's buttons. Don't Allow is the default and what Escape picks.
const (
	allowButton = "Allow"
	denyButton  = "Don’t Allow"
)

// giveUpSeconds closes a dialog no one answers, should the agent's own
// limit not arrive first.
const giveUpSeconds = "30"

// quote makes text an AppleScript string literal.
func quote(text string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text) + `"`
}

// dialogScript asks the question with Allow and Don't Allow buttons.
func dialogScript(question string) string {
	return "display dialog " + quote(question) + ` with title "Sentinel" with icon caution` +
		" buttons {" + quote(denyButton) + ", " + quote(allowButton) + "}" +
		" default button " + quote(denyButton) + " cancel button " + quote(denyButton) +
		" giving up after " + giveUpSeconds
}

// allowed reads osascript's output for the dialog: only a click on Allow,
// not a timeout, allows.
func allowed(output string) bool {
	output = strings.TrimSpace(output)
	return strings.Contains(output, "button returned:"+allowButton) && !strings.Contains(output, "gave up:true")
}

// notificationScript shows a passing notification.
func notificationScript(text string) string {
	return "display notification " + quote(text) + ` with title "Sentinel"`
}

// SPDX-License-Identifier: Apache-2.0

package macui

import "testing"

func TestDialogScriptQuotesTheQuestion(t *testing.T) {
	got := dialogScript(`Ann "the admin" \ wants to see this screen. Allow it?`)
	want := `display dialog "Ann \"the admin\" \\ wants to see this screen. Allow it?" with title "Sentinel" with icon caution` +
		` buttons {"Don’t Allow", "Allow"} default button "Don’t Allow" cancel button "Don’t Allow" giving up after 30`
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
	if got := notificationScript(`"x"`); got != `display notification "\"x\"" with title "Sentinel"` {
		t.Errorf("notification = %s", got)
	}
}

func TestOnlyAClickOnAllowAllows(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "allows a click on Allow", output: "button returned:Allow, gave up:false\n", want: true},
		{name: "refuses a dialog that gave up", output: "button returned:, gave up:true", want: false},
		{name: "refuses Don't Allow", output: "button returned:Don’t Allow, gave up:false", want: false},
		{name: "refuses no output", output: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := allowed(test.output); got != test.want {
				t.Errorf("allowed(%q) = %v", test.output, got)
			}
		})
	}
}

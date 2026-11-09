// SPDX-License-Identifier: Apache-2.0

package protocol_test

import (
	"testing"

	"github.com/aazam476/sentinel/internal/protocol"
)

func TestScriptResult(t *testing.T) {
	cases := []struct {
		name   string
		result protocol.ScriptResult
		reason string
	}{
		{"an exit code alone", protocol.ScriptResult{ExitCode: 3}, `{"exitCode":3}`},
		{"a time limit", protocol.ScriptResult{ExitCode: -1, TimedOut: true, Error: "stopped"}, `{"exitCode":-1,"timedOut":true,"error":"stopped"}`},
	}
	for _, tc := range cases {
		t.Run("carries "+tc.name+" in the close reason and back", func(t *testing.T) {
			if got := tc.result.Reason(); got != tc.reason {
				t.Errorf("reason %s, want %s", got, tc.reason)
			}
			if got := protocol.ParseScriptResult(tc.reason); got != tc.result {
				t.Errorf("parsed %+v, want %+v", got, tc.result)
			}
		})
	}

	t.Run("reads a reason that isn't a result, such as a dropped connection, as an error", func(t *testing.T) {
		got := protocol.ParseScriptResult("the agent disconnected")
		if got != (protocol.ScriptResult{ExitCode: -1, Error: "the agent disconnected"}) {
			t.Errorf("got %+v", got)
		}
	})
}

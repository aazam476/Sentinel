// SPDX-License-Identifier: Apache-2.0

package protocol

import "encoding/json"

// ActionScriptRun runs a script (§9.8). The agent answers at once, sends
// the script's output on the stream as it comes, and closes the stream
// with the result as the reason (ScriptResult). The server closing the
// stream stops the script.
const ActionScriptRun = "script.run"

// Script languages.
const (
	LanguageBash       = "bash"
	LanguageSh         = "sh"
	LanguagePowerShell = "powershell"
	LanguagePython     = "python"
)

// ScriptRunPayload asks the agent to run a script.
type ScriptRunPayload struct {
	StreamID StreamID `json:"streamId"`
	Language string   `json:"language"`
	Body     string   `json:"body"`
	// TimeoutSeconds stops the script after this long.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// ScriptResult is how a script ended, sent as its stream's close reason.
type ScriptResult struct {
	// ExitCode is the script's exit status; -1 if it didn't exit itself.
	ExitCode int  `json:"exitCode"`
	TimedOut bool `json:"timedOut,omitempty"`
	// Error says why the script couldn't run or was stopped, if it
	// didn't simply exit.
	Error string `json:"error,omitempty"`
}

// Reason encodes the result as a stream.close reason.
func (r ScriptResult) Reason() string {
	encoded, _ := json.Marshal(r) // a struct of plain fields always encodes
	return string(encoded)
}

// ParseScriptResult reads a script stream's close reason. A reason that
// isn't a result (the connection dropped, say) is reported as an error.
func ParseScriptResult(reason string) ScriptResult {
	var result ScriptResult
	if err := json.Unmarshal([]byte(reason), &result); err != nil {
		return ScriptResult{ExitCode: -1, Error: reason}
	}
	return result
}

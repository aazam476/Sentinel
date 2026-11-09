// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/hub"
)

// fakeScripts writes output and ends as told; with hold, it waits for the
// script to be stopped.
type fakeScripts struct {
	output string
	result protocol.ScriptResult
	err    error
	hold   bool
	ran    chan protocol.ScriptRunPayload
}

func (f *fakeScripts) Run(ctx context.Context, request protocol.ScriptRunPayload, out io.Writer) (protocol.ScriptResult, error) {
	f.ran <- request
	if f.err != nil {
		return protocol.ScriptResult{}, f.err
	}
	if _, err := io.WriteString(out, f.output); err != nil {
		return protocol.ScriptResult{}, err
	}
	if f.hold {
		<-ctx.Done()
		return protocol.ScriptResult{ExitCode: -1, Error: "the script was stopped"}, nil
	}
	return f.result, nil
}

func startWithScripts(t *testing.T, fake *fakeScripts, localPolicy policy.Policy) (*server, int64) {
	t.Helper()
	s := newServer(t)
	s.machine.Scripts = fake
	events := s.subscribe()
	agent := s.startAgent(localPolicy)
	waitFor(t, events, agent.state.AgentID, hub.EventInfoUpdated)
	stored, err := s.fleet.AgentForHandshake(t.Context(), agent.state.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	return s, stored.ID
}

func runOn(t *testing.T, s *server, agentID int64, body string) (*hub.Stream, error) {
	t.Helper()
	return s.hub.OpenStream(t.Context(), agentID, protocol.ActionScriptRun, func(id protocol.StreamID) any {
		return protocol.ScriptRunPayload{StreamID: id, Language: protocol.LanguageSh, Body: body, TimeoutSeconds: 5}
	}, nil)
}

// readAll collects a stream's output until it ends, and its reason.
func readAll(stream *hub.Stream) (string, string) {
	var output strings.Builder
	for {
		select {
		case frame := <-stream.Frames():
			output.Write(frame)
		case <-stream.Done():
			for {
				select {
				case frame := <-stream.Frames():
					output.Write(frame)
				default:
					return output.String(), stream.Reason()
				}
			}
		}
	}
}

func TestScripts(t *testing.T) {
	t.Run("runs a script, sending its output and then how it ended", func(t *testing.T) {
		fake := &fakeScripts{output: strings.Repeat("x", 100_000), result: protocol.ScriptResult{ExitCode: 3}, ran: make(chan protocol.ScriptRunPayload, 1)}
		s, agentID := startWithScripts(t, fake, policy.Policy{})
		stream, err := runOn(t, s, agentID, "echo hi")
		if err != nil {
			t.Fatal(err)
		}
		if ran := <-fake.ran; ran.Body != "echo hi" || ran.TimeoutSeconds != 5 {
			t.Errorf("ran %+v", ran)
		}
		output, reason := readAll(stream)
		if len(output) != 100_000 || protocol.ParseScriptResult(reason) != (protocol.ScriptResult{ExitCode: 3}) {
			t.Errorf("got %d bytes and %q", len(output), reason)
		}
	})

	t.Run("reports a script that couldn't start", func(t *testing.T) {
		fake := &fakeScripts{err: errors.New("python: the script's interpreter isn't installed on this machine"), ran: make(chan protocol.ScriptRunPayload, 1)}
		s, agentID := startWithScripts(t, fake, policy.Policy{})
		stream, err := runOn(t, s, agentID, "print(1)")
		if err != nil {
			t.Fatal(err)
		}
		_, reason := readAll(stream)
		if result := protocol.ParseScriptResult(reason); result.ExitCode != -1 || !strings.Contains(result.Error, "isn't installed") {
			t.Errorf("got %+v", result)
		}
	})

	t.Run("stops a script when the server closes its stream", func(t *testing.T) {
		fake := &fakeScripts{output: "started\n", hold: true, ran: make(chan protocol.ScriptRunPayload, 1)}
		s, agentID := startWithScripts(t, fake, policy.Policy{})
		stream, err := runOn(t, s, agentID, "sleep 60")
		if err != nil {
			t.Fatal(err)
		}
		<-fake.ran
		if got := string(<-stream.Frames()); got != "started\n" {
			t.Errorf("got %q", got)
		}
		stream.Close(t.Context(), "canceled by the user")
	})

	t.Run("SEC-22 refuses scripts when the local policy switches them off", func(t *testing.T) {
		localPolicy, err := policy.Parse("[capabilities]\nscripts = false\n")
		if err != nil {
			t.Fatal(err)
		}
		s, agentID := startWithScripts(t, &fakeScripts{ran: make(chan protocol.ScriptRunPayload, 1)}, localPolicy)
		if _, err := runOn(t, s, agentID, "id"); codeOf(err) != protocol.ErrorPolicyDenied {
			t.Errorf("got %v", err)
		}
	})

	t.Run("refuses a script without a stream for its output", func(t *testing.T) {
		s, agentID := startWithScripts(t, &fakeScripts{ran: make(chan protocol.ScriptRunPayload, 1)}, policy.Policy{})
		err := s.hub.Request(t.Context(), agentID, protocol.ActionScriptRun, protocol.ScriptRunPayload{Language: "sh", Body: "id"}, nil)
		if codeOf(err) != protocol.ErrorInvalidRequest {
			t.Errorf("got %v", err)
		}
	})
}

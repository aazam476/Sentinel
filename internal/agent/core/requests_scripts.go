// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"io"

	"github.com/aazam476/sentinel/internal/agent/policy"
	"github.com/aazam476/sentinel/internal/protocol"
)

// Scripts runs scripts (internal/agent/scripts).
type Scripts interface {
	Run(ctx context.Context, request protocol.ScriptRunPayload, out io.Writer) (protocol.ScriptResult, error)
}

// maxScriptFrame is the most output one frame carries.
const maxScriptFrame = 32 << 10

// runScript starts a script (§9.8) and answers at once; the output follows
// on the stream, which closes with the result. The local policy can switch
// scripts off (SEC-22).
func (c *connection) runScript(ctx context.Context, request protocol.Message) (any, *protocol.Error) {
	if refusal := c.refuseUnlessAllowed(policy.Scripts, "Running scripts"); refusal != nil {
		return nil, refusal
	}
	var payload protocol.ScriptRunPayload
	if err := protocol.DecodePayload(request, &payload); err != nil {
		return nil, invalidRequest(err)
	}
	if payload.StreamID == 0 {
		return nil, &protocol.Error{Code: protocol.ErrorInvalidRequest, Message: "script.run needs a stream for the output."}
	}
	out := scriptOutput{ctx: ctx, stream: payload.StreamID, conn: streamOutput{conn: c}}
	c.follow(ctx, payload.StreamID, func(ctx context.Context) string {
		out.ctx = ctx
		result, err := c.agent.options.Scripts.Run(ctx, payload, &out)
		if err != nil {
			result = protocol.ScriptResult{ExitCode: -1, Error: err.Error()}
		}
		return result.Reason()
	})
	return struct{}{}, nil
}

// scriptOutput sends a script's output as stream frames.
type scriptOutput struct {
	ctx    context.Context //nolint:containedctx // the stream's context, for writes the script makes
	stream protocol.StreamID
	conn   streamOutput
}

func (o *scriptOutput) Write(data []byte) (int, error) {
	for start := 0; start < len(data); start += maxScriptFrame {
		chunk := data[start:min(start+maxScriptFrame, len(data))]
		if err := o.conn.SendFrame(o.ctx, o.stream, chunk); err != nil {
			return start, err
		}
	}
	return len(data), nil
}

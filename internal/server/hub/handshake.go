// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"errors"
	"fmt"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/auth"
	"github.com/aazam476/sentinel/internal/server/fleet"
)

// closeError is a failed handshake: the close code and reason to send the
// agent, and the underlying error for the server's log.
type closeError struct {
	code   websocket.StatusCode
	reason string
	err    error
}

func (e *closeError) Error() string {
	return fmt.Sprintf("%s: %v", e.reason, e.err)
}

func (e *closeError) Unwrap() error {
	return e.err
}

func refuse(code websocket.StatusCode, reason string, err error) error {
	return &closeError{code: code, reason: reason, err: err}
}

// greeting is what an agent opens with: enroll on its first connection,
// hello on every one after.
type greeting struct {
	enroll *protocol.EnrollPayload
	hello  *protocol.HelloPayload
}

// handshake runs enrollment, if the agent asks for it, and the mutual
// authentication of §7.3. It returns the agent once both sides have proven
// who they are.
func (h *Hub) handshake(ctx context.Context, conn *websocket.Conn, ip string) (fleet.Agent, protocol.HelloPayload, error) {
	first, err := readGreeting(ctx, conn)
	if err != nil {
		return fleet.Agent{}, protocol.HelloPayload{}, err
	}
	if first.enroll != nil {
		if err := h.enroll(ctx, conn, *first.enroll, ip); err != nil {
			return fleet.Agent{}, protocol.HelloPayload{}, err
		}
		// The new agent carries on with the handshake on this connection.
		var hello protocol.HelloPayload
		if err := protocol.ReadPayload(ctx, conn, protocol.TypeHello, &hello); err != nil {
			return fleet.Agent{}, protocol.HelloPayload{}, refuse(protocol.CloseHandshakeFailed, "expected hello", err)
		}
		first.hello = &hello
	}
	agent, err := h.authenticate(ctx, conn, *first.hello)
	return agent, *first.hello, err
}

func readGreeting(ctx context.Context, conn *websocket.Conn) (greeting, error) {
	message, err := protocol.ReadMessage(ctx, conn)
	if err != nil {
		return greeting{}, refuse(protocol.CloseHandshakeFailed, "expected enroll or hello", err)
	}
	switch message.Type {
	case protocol.TypeEnroll:
		var payload protocol.EnrollPayload
		err = protocol.DecodePayload(message, &payload)
		return greeting{enroll: &payload}, wrapGreetingError(err)
	case protocol.TypeHello:
		var payload protocol.HelloPayload
		err = protocol.DecodePayload(message, &payload)
		return greeting{hello: &payload}, wrapGreetingError(err)
	default:
		return greeting{}, refuse(protocol.CloseHandshakeFailed, "expected enroll or hello",
			fmt.Errorf("got %s", message.Type))
	}
}

func wrapGreetingError(err error) error {
	if err != nil {
		return refuse(protocol.CloseHandshakeFailed, "malformed greeting", err)
	}
	return nil
}

// enroll stores a new agent and tells it its ID and the server's key.
func (h *Hub) enroll(ctx context.Context, conn *websocket.Conn, payload protocol.EnrollPayload, ip string) error {
	if err := checkVersion(payload.ProtocolVersion); err != nil {
		return err
	}
	agent, err := h.fleet.Enroll(ctx, fleet.EnrollInput{
		Token: payload.Token, PublicKey: payload.PublicKey, Hostname: payload.Hostname, OS: payload.OS,
		Arch: payload.Arch, AgentVersion: payload.AgentVersion, ProtocolVersion: payload.ProtocolVersion, IP: ip,
	})
	var validation *auth.ValidationError
	switch {
	case errors.Is(err, fleet.ErrEnrollmentRefused):
		return refuse(protocol.CloseEnrollmentRefused, "the enrollment token is invalid, expired, revoked or used up", err)
	case errors.As(err, &validation):
		return refuse(protocol.CloseHandshakeFailed, "invalid enrollment: "+validation.Error(), err)
	case err != nil:
		return refuse(websocket.StatusInternalError, "the server couldn't enroll the agent", err)
	}
	h.logger.Info("agent enrolled", "agent", agent.PublicID, "hostname", agent.Hostname, "ip", ip)
	enrolled := protocol.EnrolledPayload{AgentID: agent.PublicID, ServerPublicKey: h.key.Public()}
	return protocol.WriteMessage(ctx, conn, protocol.Message{Type: protocol.TypeEnrolled}, enrolled)
}

// authenticate is the challenge and proof of §7.3: the server signs first,
// so the agent can tell an impostor before proving anything itself.
func (h *Hub) authenticate(ctx context.Context, conn *websocket.Conn, hello protocol.HelloPayload) (fleet.Agent, error) {
	if err := checkVersion(hello.ProtocolVersion); err != nil {
		return fleet.Agent{}, err
	}
	if len(hello.AgentNonce) != protocol.NonceSize {
		return fleet.Agent{}, refuse(protocol.CloseHandshakeFailed, "malformed hello", errors.New("wrong nonce size"))
	}
	agent, err := h.fleet.AgentForHandshake(ctx, hello.AgentID)
	switch {
	case errors.Is(err, fleet.ErrUnknownAgent):
		return fleet.Agent{}, refuse(protocol.CloseUnknownAgent, "the server doesn't know this agent; enroll it again", err)
	case errors.Is(err, fleet.ErrAgentRevoked):
		return fleet.Agent{}, refuse(protocol.CloseRevoked, "this agent was revoked", err)
	case err != nil:
		return fleet.Agent{}, refuse(websocket.StatusInternalError, "the server couldn't look up the agent", err)
	}

	serverNonce := protocol.NewNonce()
	challenge := protocol.ChallengePayload{
		ServerNonce: serverNonce,
		Signature:   protocol.SignServerChallenge(h.key.Private(), hello.AgentNonce, serverNonce, agent.PublicID),
	}
	if err := protocol.WriteMessage(ctx, conn, protocol.Message{Type: protocol.TypeChallenge}, challenge); err != nil {
		return fleet.Agent{}, err
	}
	var prove protocol.ProvePayload
	if err := protocol.ReadPayload(ctx, conn, protocol.TypeProve, &prove); err != nil {
		return fleet.Agent{}, refuse(protocol.CloseHandshakeFailed, "expected prove", err)
	}
	err = protocol.VerifyAgentProof(agent.PublicKey, serverNonce, hello.AgentNonce, agent.PublicID, prove.Signature)
	if err != nil {
		return fleet.Agent{}, refuse(protocol.CloseHandshakeFailed, "the agent's signature is invalid", err)
	}
	return agent, nil
}

func checkVersion(version int) error {
	if version < protocol.OldestSupportedVersion || version > protocol.CurrentVersion {
		return refuse(protocol.CloseUnsupportedVersion,
			fmt.Sprintf("this server speaks protocol versions %d to %d", protocol.OldestSupportedVersion, protocol.CurrentVersion),
			fmt.Errorf("agent speaks version %d", version))
	}
	return nil
}

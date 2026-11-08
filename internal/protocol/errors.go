// SPDX-License-Identifier: Apache-2.0

package protocol

// ErrorCode is a machine-readable reason a request failed. The list is
// fixed: both sides and the web UI switch on these values.
type ErrorCode string

// The error codes.
const (
	// ErrorNotFound: the thing the request names doesn't exist.
	ErrorNotFound ErrorCode = "not_found"
	// ErrorPolicyDenied: the machine's local policy file forbids this.
	ErrorPolicyDenied ErrorCode = "policy_denied"
	// ErrorUnsupportedPlatform: this operating system can't do this.
	ErrorUnsupportedPlatform ErrorCode = "unsupported_platform"
	// ErrorTimeout: the request took too long.
	ErrorTimeout ErrorCode = "timeout"
	// ErrorInvalidRequest: the request was malformed.
	ErrorInvalidRequest ErrorCode = "invalid_request"
	// ErrorUnknownAction: the agent doesn't know this action (an older agent).
	ErrorUnknownAction ErrorCode = "unknown_action"
	// ErrorInternal: something unexpected failed; details are in the log.
	ErrorInternal ErrorCode = "internal"
	// ErrorConflict: the thing is in a state that doesn't allow this, such
	// as removing a running container.
	ErrorConflict ErrorCode = "conflict"
	// ErrorDockerUnavailable: the agent found no Docker (or Podman) socket,
	// or couldn't talk to it.
	ErrorDockerUnavailable ErrorCode = "docker_unavailable"
)

// Error is the error of a failed response.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *Error) Error() string {
	return string(e.Code) + ": " + e.Message
}

# Protocol golden fixtures

One file per message type, exactly as it travels over the agent's WebSocket. Both the
server's and the agent's tests decode every file here (see `internal/protocol/golden_test.go`
and the agent and hub tests), so the two sides can't disagree about the wire format without a
test failing.

Binary values (keys, nonces, signatures) are base64, as Go's JSON encoding writes `[]byte`.
The keys and signatures here are examples, not real credentials.

When a message changes, update its file here first; old versions stay accepted as long as the
server supports that protocol version.

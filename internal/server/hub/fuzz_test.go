// SPDX-License-Identifier: Apache-2.0

package hub_test

import (
	"testing"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
)

// FuzzHandshakeGreeting sends arbitrary bytes as an agent's first message.
// Whatever they are, the server must refuse the connection cleanly: never
// crash, hang or let the sender count as online.
func FuzzHandshakeGreeting(f *testing.F) {
	seeds := []string{
		`{"type":"hello","payload":{"agentId":"01JBX3K5Q7Z8M4N6P8R0T2V4W6","agentVersion":"v1","protocolVersion":1,"agentNonce":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}}`,
		`{"type":"enroll","payload":{"token":"x","publicKey":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=","hostname":"h","os":"linux","arch":"amd64","agentVersion":"v1","protocolVersion":1}}`,
		`{"type":"prove","payload":{"signature":""}}`,
		`{"type":"hello","payload":null}`,
		`{"type":"hello","payload":{"protocolVersion":-1}}`,
		`[]`,
		``,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	h := newHarness(f)
	f.Fuzz(func(t *testing.T, greeting []byte) {
		conn, _, err := websocket.Dial(t.Context(), h.url, nil) //nolint:bodyclose // coder/websocket closes the response body
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow() //nolint:errcheck // the test is over either way
		if err := conn.Write(t.Context(), websocket.MessageText, greeting); err != nil {
			return // the server already closed the connection
		}
		for {
			message, err := protocol.ReadMessage(t.Context(), conn)
			if err != nil {
				return // refused, as it should be
			}
			if message.Type == protocol.TypeWelcome {
				t.Fatalf("greeting %q was welcomed", greeting)
			}
		}
	})
}

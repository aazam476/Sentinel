// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bufio"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aazam476/sentinel/internal/protocol"
	"github.com/aazam476/sentinel/internal/server/api"
)

// signedOut is why a connection ends when its session no longer allows it.
const signedOut = "the viewer was signed out or lost access"

// watchedTimers is how many of the fake clock's timers are armed while a
// machine has one watched connection open and nothing else is going on:
// the machine's heartbeat watch and the connection's session check.
const watchedTimers = 2

// settle waits until exactly watchedTimers timers are armed. An answer
// from the server can reach the test before the server has disarmed that
// write's time limit; advancing the clock in that gap would fire it, and
// the machine's connection would close as if the write had hung.
func (s *testServer) settle(t *testing.T) {
	t.Helper()
	if err := s.clock.WaitForArmed(t.Context(), watchedTimers); err != nil {
		t.Fatal(err)
	}
}

// secondBrowser signs the admin in again, in another browser.
func (s *testServer) secondBrowser(admin admin) *browser {
	s.t.Helper()
	other := s.newBrowser()
	expect(s.t, other.do(http.MethodPost, "/api/auth/login", api.LoginRequest{Username: "admin", Password: strongPassword}), http.StatusOK, "")
	expect(s.t, other.do(http.MethodPost, "/api/auth/login/second-factor", api.SecondFactorRequest{TOTPCode: s.code(admin.totpSecret)}), http.StatusOK, "")
	return other
}

// otherSession is the admin's session in the browser that isn't asking.
func otherSession(t *testing.T, asking *browser) api.SessionSummary {
	t.Helper()
	var sessions api.SessionsResponse
	asking.do(http.MethodGet, "/api/auth/sessions", nil).decode(t, &sessions)
	for _, session := range sessions.Sessions {
		if !session.Current {
			return session
		}
	}
	t.Fatalf("no other session among %+v", sessions.Sessions)
	return api.SessionSummary{}
}

// streamClosed reads on as the machine until the server ends a stream, and
// fails the test if it doesn't within ten seconds.
func (f terminalFixture) streamClosed(t *testing.T) protocol.StreamClosePayload {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		message, frame, err := protocol.ReadAny(ctx, f.agent)
		if err != nil {
			t.Fatalf("the server never ended the stream on the machine: %v", err)
		}
		if frame != nil || message.Type != protocol.TypeStreamClose {
			continue
		}
		var closed protocol.StreamClosePayload
		if err := protocol.DecodePayload(message, &closed); err != nil {
			t.Fatal(err)
		}
		return closed
	}
}

// agentPing sends the machine's heartbeat and waits for the answer, so the
// server doesn't take a quiet test machine for a dead one.
func (f terminalFixture) agentPing(t *testing.T) {
	t.Helper()
	ping := protocol.Message{Type: protocol.TypePing, ID: protocol.NewID(f.server.clock.Now())}
	if err := protocol.WriteMessage(t.Context(), f.agent, ping, nil); err != nil {
		t.Fatal(err)
	}
	f.agentRead(t, protocol.TypePong)
}

// sessionEnding is one way a session ends, or loses what a shell needs,
// while a connection it opened is still up.
type sessionEnding struct {
	name string
	// prepare returns the browser that opens the connection, and what
	// then takes its session away.
	prepare func(t *testing.T, f terminalFixture) (viewer *browser, end func())
}

// asOperator is an ending an admin brings on an operator by changing them.
func asOperator(change api.UpdateUserRequest) func(t *testing.T, f terminalFixture) (*browser, func()) {
	return func(t *testing.T, f terminalFixture) (*browser, func()) {
		operator := f.server.addUser(f.admin, "olu", "operator")
		return operator, func() {
			expect(t, f.admin.browser.do(http.MethodPatch, "/api/users/"+userID(t, f.admin, "olu"), change), http.StatusOK, "")
		}
	}
}

func signingOut() sessionEnding {
	return sessionEnding{"they sign out", func(t *testing.T, f terminalFixture) (*browser, func()) {
		return f.admin.browser, func() {
			expect(t, f.admin.browser.do(http.MethodPost, "/api/auth/logout", nil), http.StatusNoContent, "")
		}
	}}
}

func revokingTheSession() sessionEnding {
	return sessionEnding{"they revoke the session from another browser", func(t *testing.T, f terminalFixture) (*browser, func()) {
		other := f.server.secondBrowser(f.admin)
		return f.admin.browser, func() {
			path := "/api/auth/sessions/" + strconv.FormatInt(otherSession(t, other).ID, 10)
			expect(t, other.do(http.MethodDelete, path, nil), http.StatusNoContent, "")
		}
	}}
}

func disablingTheUser() sessionEnding {
	disabled := true
	return sessionEnding{"an admin disables them", asOperator(api.UpdateUserRequest{Disabled: &disabled})}
}

func takingTheRoleAway() sessionEnding {
	viewerRole := "viewer"
	return sessionEnding{"an admin makes them only a viewer", asOperator(api.UpdateUserRequest{Role: &viewerRole})}
}

func TestSEC06_ATerminalEndsWithTheSessionThatOpenedIt(t *testing.T) {
	for _, ending := range []sessionEnding{signingOut(), revokingTheSession(), disablingTheUser(), takingTheRoleAway()} {
		t.Run("when "+ending.name, func(t *testing.T) {
			f := newTerminalFixture(t)
			viewer, end := ending.prepare(t, f)
			browser, _, err := f.dialTerminal(t, viewer, "")
			if err != nil {
				t.Fatal(err)
			}
			open := f.acceptOpen(t, "watched", false)
			browserStatus(t, browser)

			end()
			f.server.settle(t)
			f.server.clock.Advance(30 * time.Second)

			// The shell ends on the machine, for good, and the browser hears why.
			if closed := f.streamClosed(t); closed.StreamID != open.StreamID || closed.KeepSession || closed.Reason != signedOut {
				t.Errorf("the machine was told %+v, want the shell ended for good", closed)
			}
			if status := browserStatus(t, browser); status.Type != "closed" || status.Reason != signedOut {
				t.Errorf("the browser was told %+v", status)
			}
			// The relay closes the browser's connection, which the browser
			// answers by reading on, just before the end is audited.
			for err == nil {
				_, _, err = browser.Read(t.Context())
			}
			want := []string{"terminal.open:started", "terminal.close:success"}
			if got := eventualAuditActions(t, f.server, "terminal.", want); !slices.Equal(got, want) {
				t.Errorf("audit = %v, want %v", got, want)
			}
			// No one picks the shell up again.
			if _, status, err := f.dialTerminal(t, f.admin.browser, "?resume=watched"); err == nil {
				t.Errorf("the ended shell was resumed (status %d)", status)
			}
		})
	}

	t.Run("and stays open while the session is good, which counts as using it", func(t *testing.T) {
		f := newTerminalFixture(t)
		other := f.server.secondBrowser(f.admin)
		browser, _, err := f.dialTerminal(t, f.admin.browser, "")
		if err != nil {
			t.Fatal(err)
		}
		open := f.acceptOpen(t, "watched", false)
		browserStatus(t, browser)
		before := otherSession(t, other).LastSeenAt

		// Two minutes pass in which the browser with the terminal asks the
		// server nothing. The machine keeps its own connection alive.
		for range 4 {
			f.agentPing(t)
			f.server.settle(t)
			f.server.clock.Advance(30 * time.Second)
			// Both timers are set again once their work is done.
			f.server.settle(t)
		}
		if err := protocol.WriteFrame(t.Context(), f.agent, open.StreamID, []byte("still here")); err != nil {
			t.Fatal(err)
		}
		if _, data, err := browser.Read(t.Context()); err != nil || string(data) != "still here" {
			t.Fatalf("the terminal stopped relaying: %q (%v)", data, err)
		}
		if after := otherSession(t, other).LastSeenAt; after < before+time.Minute.Milliseconds() {
			t.Errorf("the session was last seen at %d, not after the terminal's checks (it was %d before): a shell in use would idle out", after, before)
		}
	})
}

func TestSEC06_ADesktopSessionEndsWithTheSessionThatOpenedIt(t *testing.T) {
	for _, ending := range []sessionEnding{signingOut(), takingTheRoleAway()} {
		t.Run("when "+ending.name, func(t *testing.T) {
			f := newDesktopFixture(t, availableDesktop(), true)
			viewer, end := ending.prepare(t, f)
			browser, _, err := f.dialDesktop(t, viewer, "")
			if err != nil {
				t.Fatal(err)
			}
			start := f.acceptDesktop(t)
			if _, _, err := browser.Read(t.Context()); err != nil { // the "ready" message
				t.Fatal(err)
			}

			end()
			f.server.settle(t)
			f.server.clock.Advance(30 * time.Second)

			if closed := f.streamClosed(t); closed.StreamID != start.StreamID || closed.Reason != signedOut {
				t.Errorf("the machine was told %+v, want the screen no longer shared", closed)
			}
			_, _, err = browser.Read(t.Context())
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure || !strings.Contains(err.Error(), "signed out or lost access") {
				t.Errorf("the browser's connection ended with %v", err)
			}
			want := []string{"desktop.open:started", "desktop.close:success"}
			if got := eventualAuditActions(t, f.server, "desktop.", want); !slices.Equal(got, want) {
				t.Errorf("audit = %v, want %v", got, want)
			}
		})
	}
}

func TestSEC06_AFollowedLogEndsWithTheSessionThatOpenedIt(t *testing.T) {
	f := newTerminalFixture(t)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.server.server.URL+dockerPath(f, "/containers/web/logs/follow"), nil)
	if err != nil {
		t.Fatal(err)
	}
	following := make(chan *http.Response, 1)
	go func() {
		response, err := f.admin.browser.client.Do(request) //nolint:bodyclose // closed below
		if err != nil {
			t.Error(err)
		}
		following <- response
	}()
	open := f.agentRead(t, protocol.TypeRequest)
	var payload protocol.ContainerLogsPayload
	if err := protocol.DecodePayload(open, &payload); err != nil {
		t.Fatal(err)
	}
	recent := protocol.ContainerLogsResultPayload{Lines: []protocol.ContainerLogLine{{Time: 1, Stream: "stdout", Text: "old"}}}
	if err := protocol.WriteMessage(t.Context(), f.agent, protocol.Message{Type: protocol.TypeResponse, ID: open.ID}, recent); err != nil {
		t.Fatal(err)
	}
	response := <-following
	defer response.Body.Close() //nolint:errcheck // nothing to report
	lines := bufio.NewScanner(response.Body)
	lines.Scan() // the first line arrived: the log is being followed

	expect(t, f.admin.browser.do(http.MethodPost, "/api/auth/logout", nil), http.StatusNoContent, "")
	f.server.settle(t)
	f.server.clock.Advance(30 * time.Second)

	if closed := f.streamClosed(t); closed.StreamID != payload.StreamID || closed.Reason != signedOut {
		t.Errorf("the machine was told %+v, want it to stop following", closed)
	}
	for lines.Scan() { // the server ends the response
	}
}

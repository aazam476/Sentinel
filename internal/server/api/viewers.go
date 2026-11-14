// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/aazam476/sentinel/internal/server/auth"
)

// A terminal, a desktop session or a followed log stays open long after
// the request that started it was checked. Such a connection must not
// outlive the session that opened it (SEC-06): signing out, revoking the
// session, disabling the user or taking their role away has to close the
// root shell too, not only stop new requests. So each one checks its
// session again every sessionCheckInterval, as the event stream does.
// See docs/decisions/0044-connections-end-with-their-session.md.

// sessionCheckInterval is how often a long-lived connection checks the
// session that opened it.
const sessionCheckInterval = 30 * time.Second

// signedOutReason is why a connection ends when its session no longer
// allows it. The viewer and the audit log both see it.
const signedOutReason = "the viewer was signed out or lost access"

// viewer is who opened a long-lived connection.
type viewer struct {
	// session is their session as it was when the connection opened.
	session auth.Session
	// token is the session's token, to check it again with.
	token string
	// client is where they connected from, for the audit log.
	client auth.RequestInfo
}

// viewerOf describes who sent r, already signed in as session.
func viewerOf(r *http.Request, session auth.Session) viewer {
	who := viewer{session: session, client: requestInfo(r)}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		who.token = cookie.Value
	}
	return who
}

// sessionWatch is what a long-lived connection needs of its session.
type sessionWatch struct {
	// permission is what the session's role must still allow.
	permission auth.Permission
	// inUse makes the open connection count as the session being used, so
	// the idle timeout doesn't end a shell someone left a job running in.
	// The session's seven-day limit ends it all the same.
	inUse bool
}

// endWithSession calls end, once, as soon as a check finds the viewer's
// session over or without the permission the connection needs. The checks
// run until the returned stop is called, which waits for them to finish.
func (h *handlers) endWithSession(ctx context.Context, who viewer, watch sessionWatch, end func(reason string)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	// The timer starts here, not in the goroutine, so the first check is
	// due one interval after the connection opened, however late the
	// goroutine gets to run.
	timer := h.clock.NewTimer(sessionCheckInterval)
	finished := make(chan struct{})
	// Lifetime: until stop is called or the session no longer allows the
	// connection, whichever comes first.
	go func() {
		defer close(finished)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C():
			}
			allowed := h.stillAllowed(ctx, who, watch)
			if ctx.Err() != nil {
				return // the connection ended by itself meanwhile
			}
			if !allowed {
				end(signedOutReason)
				return
			}
			timer.Reset(sessionCheckInterval)
		}
	}()
	return func() {
		cancel()
		<-finished
	}
}

// stillAllowed reads the viewer's session again: it must still be signed
// in, with a role that has the permission. A session that can't be read
// isn't trusted.
func (h *handlers) stillAllowed(ctx context.Context, who viewer, watch sessionWatch) bool {
	check := h.auth.CheckSession
	if watch.inUse {
		check = h.auth.Authenticate
	}
	session, err := check(ctx, who.token)
	if err != nil {
		if !errors.Is(err, auth.ErrNotAuthenticated) && ctx.Err() == nil {
			h.logger.Error("could not check a session again; ending its connection", "user", who.session.Username, "error", err)
		}
		return false
	}
	return session.IsSignedIn() && session.Role.Can(watch.permission)
}

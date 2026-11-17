// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aazam476/sentinel/internal/server/audit"
	"github.com/aazam476/sentinel/internal/server/auth"
)

func TestSEC05_FailedLoginsBackOffExponentially(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	admin, _, _ := h.setUpAdmin(t)
	wrong := auth.LoginInput{Username: admin.username, Password: "not the right password"}

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := h.service.Login(t.Context(), wrong, testClient()); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("attempt %d error = %v, want ErrInvalidCredentials without delay", attempt, err)
		}
	}

	t.Run("makes the client wait after three failures, even with the right password", func(t *testing.T) {
		_, err := h.service.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: admin.password}, testClient())
		var tooMany *auth.TooManyAttemptsError
		if !errors.As(err, &tooMany) || tooMany.RetryAfter != time.Second {
			t.Errorf("error = %v, want a 1 s wait", err)
		}
	})

	t.Run("doubles the wait with each further failure", func(t *testing.T) {
		h.clock.Advance(time.Second)
		if _, err := h.service.Login(t.Context(), wrong, testClient()); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("fourth attempt error = %v", err)
		}
		_, err := h.service.Login(t.Context(), wrong, testClient())
		var tooMany *auth.TooManyAttemptsError
		if !errors.As(err, &tooMany) || tooMany.RetryAfter != 2*time.Second {
			t.Errorf("error = %v, want a 2 s wait", err)
		}
	})

	t.Run("applies to the IP address across usernames", func(t *testing.T) {
		_, err := h.service.Login(t.Context(), auth.LoginInput{Username: "someone-else", Password: "whatever-password"}, testClient())
		var tooMany *auth.TooManyAttemptsError
		if !errors.As(err, &tooMany) {
			t.Errorf("error = %v, want the IP address to be in backoff", err)
		}
	})

	t.Run("audits every failure and refusal", func(t *testing.T) {
		actions := h.auditActions(t)
		if countOf(actions, "auth.login.password:failure") != 4 || !contains(actions, "auth.login:denied") {
			t.Errorf("audit = %v, want 4 password failures and the refusals", actions)
		}
	})
}

func TestSEC05_LocksAccountAfterTenFailures(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	admin, _, _ := h.setUpAdmin(t)

	var lastErr error
	for attempt := 1; attempt <= 10; attempt++ {
		h.clock.Advance(maxWait())
		// A different IP each time, so only the username limit applies.
		client := auth.RequestInfo{IP: "198.51.100." + string(rune('0'+attempt%10))}
		_, lastErr = h.service.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: "wrong password!!"}, client)
	}

	t.Run("the tenth failure locks the account for 15 minutes", func(t *testing.T) {
		var tooMany *auth.TooManyAttemptsError
		if !errors.As(lastErr, &tooMany) || tooMany.RetryAfter != 15*time.Minute {
			t.Errorf("tenth failure error = %v, want a 15 minute lock", lastErr)
		}
		if !contains(h.auditActions(t), "auth.account.lock:success") {
			t.Errorf("audit = %v, want the lock recorded", h.auditActions(t))
		}
	})

	t.Run("the right password is refused while locked, even after a restart", func(t *testing.T) {
		h.clock.Advance(time.Minute)
		restarted := h.restart(t)
		_, err := restarted.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: admin.password}, auth.RequestInfo{IP: "192.0.2.50"})
		var tooMany *auth.TooManyAttemptsError
		if !errors.As(err, &tooMany) || tooMany.RetryAfter != 14*time.Minute {
			t.Errorf("error = %v, want 14 minutes left", err)
		}
	})

	t.Run("the account opens again after 15 minutes", func(t *testing.T) {
		h.clock.Advance(15 * time.Minute)
		_, err := h.service.Login(t.Context(), auth.LoginInput{Username: admin.username, Password: admin.password}, auth.RequestInfo{IP: "192.0.2.51"})
		if err != nil {
			t.Errorf("Login after the lock returned %v", err)
		}
	})
}

func TestSEC05_UnknownUsernamesAreLimitedToo(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setUpAdmin(t)

	for attempt := 1; attempt <= 3; attempt++ {
		_, _ = h.service.Login(t.Context(), auth.LoginInput{Username: "ghost", Password: "whatever-password"}, testClient()) // failures expected
	}
	_, err := h.service.Login(t.Context(), auth.LoginInput{Username: "ghost", Password: "whatever-password"}, testClient())

	var tooMany *auth.TooManyAttemptsError
	if !errors.As(err, &tooMany) {
		t.Errorf("error = %v, want backoff for an unknown username like for a real one", err)
	}
}

// maxWait is longer than any backoff before a lock.
func maxWait() time.Duration {
	return 16 * time.Minute
}

func TestSEC40_RefusedLoginsCantFillTheAuditLog(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setUpAdmin(t)
	for index := range 300 {
		client := auth.RequestInfo{IP: fmt.Sprintf("2001:db8:%x::1", index), UserAgent: "flood"}
		for range 5 {
			_, _ = h.service.Login(t.Context(), auth.LoginInput{Username: "nobody", Password: "wrong-password-here"}, client)
		}
	}
	rows := 0
	for _, action := range h.auditActions(t) {
		if strings.HasPrefix(action, "auth.login") {
			rows++
		}
	}
	if rows == 0 || rows > 120 {
		t.Errorf("1,500 refused logins wrote %d rows, want between 1 and 120 (a minute's cap per kind)", rows)
	}

	t.Run("and the next row says how many it stood in for", func(t *testing.T) {
		h.clock.Advance(time.Hour)
		_, _ = h.service.Login(t.Context(), auth.LoginInput{Username: "nobody", Password: "wrong-password-here"},
			auth.RequestInfo{IP: "2001:db8:12b::1", UserAgent: "flood"}) // the last network, held back by the cap
		var last audit.Record
		_ = h.auditLog.All(t.Context(), func(record audit.Record) error { last = record; return nil })
		if !strings.Contains(string(last.Details), `"heldBack":`) {
			t.Errorf("last row = %s, want a held-back count", last.Details)
		}
	})
}

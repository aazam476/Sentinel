// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"fmt"
	"time"
)

// Errors the API turns into responses. Each is never reassigned; callers
// compare with errors.Is (or errors.As for the struct types).
var (
	// ErrInvalidCredentials: wrong username or password. Deliberately doesn't
	// say which, so usernames can't be discovered.
	ErrInvalidCredentials = errors.New("the username or password is incorrect")
	// ErrInvalidCode: a TOTP or recovery code was wrong, expired or already used.
	ErrInvalidCode = errors.New("the code is incorrect or has already been used")
	// ErrSetupNotAvailable: setup is already done.
	ErrSetupNotAvailable = errors.New("setup has already been completed")
	// ErrSetupExpired: the setup was started too long ago or the server restarted.
	ErrSetupExpired = errors.New("this setup attempt has expired; start again")
	// ErrNotAuthenticated: no valid session.
	ErrNotAuthenticated = errors.New("you are not signed in")
	// ErrWrongLoginStep: the request doesn't fit where the session is in the
	// login process, for example a second-factor code for a session that
	// already passed it.
	ErrWrongLoginStep = errors.New("this step doesn't apply to your session")
	// ErrStepUpRequired: a sensitive action needs a fresh second-factor check
	// (SEC-07).
	ErrStepUpRequired = errors.New("confirm it's you with your authenticator app first")
	// ErrForbidden: the user's role doesn't allow the action.
	ErrForbidden = errors.New("your role doesn't allow this")
	// ErrNotFound: the user or session doesn't exist (or isn't yours).
	ErrNotFound = errors.New("not found")
	// ErrUsernameTaken: another user already has the name.
	ErrUsernameTaken = errors.New("that username is already taken")
	// ErrLastAdmin: the change would leave no active admin.
	ErrLastAdmin = errors.New("this would leave Sentinel without an active admin")
)

// TooManyAttemptsError means login attempts are being slowed down or the
// account is locked (SEC-05).
type TooManyAttemptsError struct {
	RetryAfter time.Duration
}

func (e *TooManyAttemptsError) Error() string {
	return fmt.Sprintf("too many failed attempts; try again in %s", e.RetryAfter.Round(time.Second))
}

// ValidationError means an input field is invalid. Message is shown to the
// user.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

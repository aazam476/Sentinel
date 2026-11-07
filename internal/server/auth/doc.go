// SPDX-License-Identifier: Apache-2.0

// Package auth decides who someone is and what they may do: the first-boot
// setup token, passwords, two-factor authentication (TOTP and recovery
// codes), sessions, login rate limiting and roles.
//
// The HTTP handlers in internal/server/api call Service, which holds the
// rules (SEC-01 to SEC-07) and records every login, failure and change in
// the audit log. The files are split by topic:
//
//	service.go          Service: the operations the API calls
//	setup.go            first-boot setup and the setup token (SEC-01, SEC-02)
//	login.go            password and second-factor login (SEC-04, SEC-05)
//	enrollment.go       TOTP enrollment for users who don't have it yet
//	sessionservice.go   checking, listing and revoking sessions (SEC-06)
//	stepup.go           fresh 2FA for sensitive actions (SEC-07)
//	userservice.go      admin user management
//	passwords.go        Argon2id hashing (SEC-03)
//	passwordpolicy.go   length and common-password checks (SEC-02)
//	totp.go             TOTP codes with replay protection
//	recoverycodes.go    one-time recovery codes
//	tokens.go           random tokens and their hashes
//	ratelimit.go        exponential backoff and account lockout (SEC-05)
//	permissions.go      the table of what each role may do
//	users.go, sessions.go, totpsecrets.go, recoverystore.go
//	                    the SQL for each table
package auth

// SPDX-License-Identifier: Apache-2.0

// Package app assembles the server from its parts: the database, the
// secrets key, the audit log, the auth service, the routes and the
// middleware around them.
//
// cmd/sentinel-server calls New once and serves App.Handler. The HTTP
// contract tests in internal/server/api call New too, so they exercise
// exactly the stack users get, including every security middleware.
package app

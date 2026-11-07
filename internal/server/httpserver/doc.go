// SPDX-License-Identifier: Apache-2.0

// Package httpserver opens the server's listening socket and serves HTTP on
// it until the server is asked to stop.
//
// cmd/sentinel-server calls Listen and then Serve. Serve hands every request
// to the handler it is given (today that is the embedded web UI from
// internal/server/web) and shuts down gracefully when its context is
// canceled, letting in-flight requests finish.
//
// TLS isn't implemented yet, so Listen only accepts loopback addresses: plain
// HTTP must never be reachable from another machine (SEC-10). See
// docs/decisions/0002-loopback-only-until-tls.md.
package httpserver

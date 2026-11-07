// SPDX-License-Identifier: Apache-2.0

// Package httpserver opens the server's listening socket and serves HTTP on
// it until the server is asked to stop.
//
// cmd/sentinel-server calls Listen, wraps the listener in TLS when HTTPS is
// on, and then calls Serve. Serve hands every request to the handler it is
// given and shuts down gracefully when its context is canceled, letting
// in-flight requests finish.
package httpserver

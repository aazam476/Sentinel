// SPDX-License-Identifier: Apache-2.0

// Package api holds the server's HTTP handlers, one file per resource, and
// NewRouter, which maps every URL to its handler.
//
// cmd/sentinel-server builds the router and wraps it in the protections from
// internal/server/middleware. Handlers call into the packages that do the
// real work (auth, store, audit, ...) and translate between JSON and Go.
//
// Routes:
//
//	GET  /healthz   "ok", for reverse proxies and monitoring (no login needed)
//	     /api/...   the JSON API
//	     /...       everything else is the web UI
package api

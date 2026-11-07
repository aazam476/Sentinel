// SPDX-License-Identifier: Apache-2.0

// Package middleware wraps the server's HTTP handlers with the protections
// every request needs, before any API handler or the web UI sees it.
//
// cmd/sentinel-server stacks them in this order, outermost first:
//
//	SecurityHeaders   CSP, framing and referrer headers on every response (SEC-11)
//	WithBasePath      serves everything under the configured base path (SEC-14)
//	WithClientInfo    works out the real client IP and scheme, believing
//	                  X-Forwarded-* only from trusted proxies (SEC-14)
//	RequireSameOrigin refuses cross-site state changes and WebSocket
//	                  upgrades by checking the Origin header (SEC-12, SEC-13)
//
// Handlers read the client's details with ClientInfoFrom.
package middleware

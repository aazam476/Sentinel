// SPDX-License-Identifier: Apache-2.0

// Package faultnet is a TCP proxy for tests that need a bad network: it
// sits between a client and a server and can cut every connection (a
// network drop or a server restart), refuse new ones (a server that is
// down), or stop traffic without closing anything (a dead link that
// TCP hasn't noticed yet).
//
// It works below TLS and WebSocket, so the code under test runs exactly as
// it would against a real network.
package faultnet

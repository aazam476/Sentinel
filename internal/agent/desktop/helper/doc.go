// SPDX-License-Identifier: Apache-2.0

// Package helper runs a desktop source in another process: the desktop
// helper (§9.3). On Windows and macOS the agent's service can't reach the
// screen of the person logged in, so it starts `sentinel-agent
// desktop-helper` in their session and drives it over the child's
// standard input and output, never a network port (SEC-26).
//
// The helper serves one request at a time. Each message is a frame: a
// big-endian uint32 length, a kind byte, then the payload. The client is
// itself a desktop.Source, so a session can't tell it from a local one.
//
//	helper.go   the message format
//	serve.go    the helper's side: answers requests with a desktop.Source
//	client.go   the agent's side: a desktop.Source backed by a helper
package helper

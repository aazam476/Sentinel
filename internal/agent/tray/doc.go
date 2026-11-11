// SPDX-License-Identifier: Apache-2.0

// Package tray connects the agent's service to the Sentinel icon in the
// Windows notification area and the macOS menu bar (ADR 0038).
//
// The service listens on a local socket, never a network port (SEC-26):
// a Unix socket at SocketPath, which on Windows only users signed in at
// the machine may open, and on macOS only the user at the console. Each
// signed-in user's tray (`sentinel-agent tray`) connects to it and shows
// whether the agent is connected and to which server, the remote sessions
// open now with a way to end each (SEC-24), and a switch to pause remote
// access. When remote desktop asks the person at the machine first
// (SEC-23), the question goes to every connected tray and the first answer
// decides; with no tray to ask, the session is refused.
//
// Messages are lines of JSON, at most 64 KiB, checked field by field
// before anything reaches the screen.
//
//	protocol.go   the messages
//	server.go     the agent's side: status, questions, commands
//	notifier.go   asking through the trays or the machine's own way
//	client.go     the tray's side: reconnects, shows, answers
//	menu.go       what the tray shows for a status
//	listen*.go    the socket and who may use it
package tray

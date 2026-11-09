// SPDX-License-Identifier: Apache-2.0

// Package update updates the agent in place (§11.3), and puts the previous
// version back if the new one can't connect (SEC-31, SEC-32).
//
// An update goes: download the build from the agent's own server; check its
// release signature against the keys compiled in (it must be the agent, for
// this system, of the version asked for); run it once as a trial, with the
// running agent's connection paused, until it completes a handshake with
// the server; only then keep the old binary as "<executable>.previous", move
// the new one into place and restart through the service manager. A build
// that fails any step is thrown away, and the agent carries on as it was.
//
// After the restart the new version watches itself: if it hasn't connected
// within five minutes it puts the previous binary back and restarts again.
// The previous binary is deleted once the new one has run for a day.
package update
